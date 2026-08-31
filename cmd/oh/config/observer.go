package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"golang.org/x/sys/unix"

	"crdx.org/io/cmd/oh/segment"
)

type snapshot struct {
	data      []byte
	failure   error
	isMissing bool
}

func readSnapshot(path string) snapshot {
	data, err := os.ReadFile(path) //nolint:gosec // the path is the configured one
	return snapshot{data: data, failure: err, isMissing: errors.Is(err, fs.ErrNotExist)}
}

func (self snapshot) equal(other snapshot) bool {
	return self.isMissing == other.isMissing &&
		bytes.Equal(self.data, other.data) &&
		errorText(self.failure) == errorText(other.failure)
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

type revision struct {
	configSnapshot       snapshot
	snippetFileSnapshots map[string]snapshot
}

func (self revision) equal(other revision) bool {
	if !self.configSnapshot.equal(other.configSnapshot) || len(self.snippetFileSnapshots) != len(other.snippetFileSnapshots) {
		return false
	}
	for path, current := range self.snippetFileSnapshots {
		previous, exists := other.snippetFileSnapshots[path]
		if !exists || !current.equal(previous) {
			return false
		}
	}
	return true
}

func (self revision) getPaths(configPath string) []string {
	paths := append([]string{configPath}, slices.Sorted(maps.Keys(self.snippetFileSnapshots))...)
	return paths
}

func readRevision(path string) (Config, revision, error) {
	current := readSnapshot(path)
	settings, err := loadSnapshot(path, current)
	return settings, revision{configSnapshot: current, snippetFileSnapshots: settings.snippetFileSnapshots}, err
}

type Observer struct {
	path            string
	handledRevision revision
	watcher         *fileWatcher
}

type ReloadStatus int

const (
	ReloadUnchanged ReloadStatus = iota
	ReloadApplied
	ReloadFailed
)

type ReloadResult struct {
	LiveConfig LiveConfig
	Status     ReloadStatus
	Failure    error
}

func Observe(path string) (Config, *Observer, error) {
	settings, current, err := readRevision(path)
	if err != nil {
		return Config{}, nil, err
	}

	watcher, err := newFileWatcher(current.getPaths(path)...)
	if err != nil {
		return Config{}, nil, fmt.Errorf("could not watch config: %w", err)
	}

	latestSettings, latest, err := readRevision(path)
	if err == nil {
		err = watcher.addPaths(latest.getPaths(path)...)
	}
	if err != nil {
		watcher.close()
		return Config{}, nil, err
	}
	if !latest.equal(current) {
		settings = latestSettings
		current = latest
	}

	return settings, &Observer{path: path, handledRevision: current, watcher: watcher}, nil
}

func (self *Observer) Changes() <-chan error {
	if self == nil {
		return nil
	}
	return self.watcher.events
}

func (self *Observer) Reload(watchFailure error, registry segment.Registry) ReloadResult {
	settings, hasChanged, err := self.refresh(watchFailure)
	if err == nil && hasChanged {
		var live LiveConfig
		live, err = settings.BuildLive(registry)
		if err == nil {
			return ReloadResult{LiveConfig: live, Status: ReloadApplied}
		}
	}
	if err != nil {
		return ReloadResult{Status: ReloadFailed, Failure: err}
	}
	return ReloadResult{Status: ReloadUnchanged}
}

func (self *Observer) Close() {
	if self != nil {
		self.watcher.close()
	}
}

func (self *Observer) refresh(watchFailure error) (Config, bool, error) {
	if self == nil {
		return Config{}, false, nil
	}
	if watchFailure != nil {
		return Config{}, false, fmt.Errorf("could not watch config: %w", watchFailure)
	}

	settings, current, err := readRevision(self.path)
	if watchErr := self.watcher.addPaths(current.getPaths(self.path)...); watchErr != nil {
		return Config{}, false, fmt.Errorf("could not watch config: %w", watchErr)
	}

	latestSettings, latest, latestErr := readRevision(self.path)
	if watchErr := self.watcher.addPaths(latest.getPaths(self.path)...); watchErr != nil {
		return Config{}, false, fmt.Errorf("could not watch config: %w", watchErr)
	}
	if !latest.equal(current) {
		settings = latestSettings
		current = latest
		err = latestErr
	}

	if current.equal(self.handledRevision) {
		return Config{}, false, nil
	}
	self.handledRevision = current
	if err != nil {
		return Config{}, false, err
	}
	return settings, true, nil
}

const watchMask = unix.IN_ATTRIB |
	unix.IN_CLOSE_WRITE |
	unix.IN_CREATE |
	unix.IN_DELETE |
	unix.IN_DELETE_SELF |
	unix.IN_MOVE_SELF |
	unix.IN_MOVED_FROM |
	unix.IN_MOVED_TO

type fileWatcher struct {
	targetPaths      map[string]struct{}
	watchDescriptors map[int]struct{}

	inotifyDescriptor int
	cancelRead        int
	cancelWrite       int

	events        chan error
	closedChannel chan struct{}
	mutex         sync.Mutex
	once          sync.Once
}

func newFileWatcher(paths ...string) (*fileWatcher, error) {
	inotifyDescriptor, err := unix.InotifyInit1(unix.IN_CLOEXEC | unix.IN_NONBLOCK)
	if err != nil {
		return nil, err
	}

	cancel := []int{0, 0}
	if err := unix.Pipe2(cancel, unix.O_CLOEXEC|unix.O_NONBLOCK); err != nil {
		_ = unix.Close(inotifyDescriptor)
		return nil, err
	}

	watcher := &fileWatcher{
		targetPaths:       make(map[string]struct{}),
		watchDescriptors:  make(map[int]struct{}),
		inotifyDescriptor: inotifyDescriptor,
		cancelRead:        cancel[0],
		cancelWrite:       cancel[1],
		events:            make(chan error, 1),
		closedChannel:     make(chan struct{}),
	}
	if err := watcher.addPaths(paths...); err != nil {
		watcher.closeDescriptors()
		return nil, err
	}

	go watcher.run()
	return watcher, nil
}

func (self *fileWatcher) addPaths(paths ...string) error {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	for _, path := range paths {
		self.targetPaths[filepath.Clean(path)] = struct{}{}
	}
	return self.armLocked()
}

func (self *fileWatcher) arm() error {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.armLocked()
}

func (self *fileWatcher) armLocked() error {
	directories := make(map[string]struct{}, len(self.targetPaths))
	for path := range self.targetPaths {
		directory, err := deepestExistingDirectory(filepath.Dir(path))
		if err != nil {
			return err
		}
		directories[directory] = struct{}{}
	}
	for directory := range directories {
		descriptor, err := unix.InotifyAddWatch(self.inotifyDescriptor, directory, watchMask)
		if err != nil {
			return err
		}
		self.watchDescriptors[descriptor] = struct{}{}
	}
	return nil
}

func deepestExistingDirectory(path string) (string, error) {
	for {
		info, err := os.Stat(path)
		if err == nil {
			if !info.IsDir() {
				return "", fmt.Errorf("%s is not a directory", path)
			}
			return path, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}

		parent := filepath.Dir(path)
		if parent == path {
			return "", err
		}
		path = parent
	}
}

func (self *fileWatcher) run() {
	defer close(self.closedChannel)
	defer close(self.events)
	defer self.closeDescriptors()

	pollDescriptors := []unix.PollFd{
		//nolint:gosec // poll uses the kernel's signed 32-bit file descriptors
		{Fd: int32(self.inotifyDescriptor), Events: unix.POLLIN},
		//nolint:gosec // poll uses the kernel's signed 32-bit file descriptors
		{Fd: int32(self.cancelRead), Events: unix.POLLIN},
	}
	buffer := make([]byte, unix.SizeofInotifyEvent*64)
	for {
		_, err := unix.Poll(pollDescriptors, -1)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			self.publishFailure(err)
			return
		}
		if pollDescriptors[1].Revents&unix.POLLIN != 0 {
			return
		}
		if pollDescriptors[0].Revents&(unix.POLLERR|unix.POLLHUP|unix.POLLNVAL) != 0 {
			self.publishFailure(errors.New("inotify stopped unexpectedly"))
			return
		}
		if pollDescriptors[0].Revents&unix.POLLIN == 0 {
			continue
		}

		if _, err := unix.Read(self.inotifyDescriptor, buffer); err != nil && !errors.Is(err, unix.EAGAIN) {
			self.publishFailure(err)
			return
		}
		if err := self.arm(); err != nil {
			self.publishFailure(err)
			return
		}
		self.publishChange()
	}
}

func (self *fileWatcher) publishChange() {
	select {
	case self.events <- nil:
	default:
	}
}

func (self *fileWatcher) publishFailure(err error) {
	select {
	case <-self.events:
	default:
	}
	self.events <- err
}

func (self *fileWatcher) close() {
	self.once.Do(func() {
		_, _ = unix.Write(self.cancelWrite, []byte{0})
		<-self.closedChannel
	})
}

func (self *fileWatcher) closeDescriptors() {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	for descriptor := range self.watchDescriptors {
		//nolint:gosec // inotify returns its watch descriptors as non-negative ints
		_, _ = unix.InotifyRmWatch(self.inotifyDescriptor, uint32(descriptor))
	}
	_ = unix.Close(self.inotifyDescriptor)
	_ = unix.Close(self.cancelRead)
	_ = unix.Close(self.cancelWrite)
}
