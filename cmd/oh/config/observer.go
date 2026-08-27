package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
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
	if errors.Is(err, fs.ErrNotExist) {
		return snapshot{isMissing: true}
	}
	return snapshot{data: data, failure: err}
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

type Observer struct {
	path    string
	handled snapshot
	watcher *fileWatcher
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
	current := readSnapshot(path)
	settings, err := loadSnapshot(path, current)
	if err != nil {
		return Config{}, nil, err
	}

	watcher, err := newFileWatcher(path)
	if err != nil {
		return Config{}, nil, fmt.Errorf("could not watch config: %w", err)
	}

	latest := readSnapshot(path)
	if !latest.equal(current) {
		settings, err = loadSnapshot(path, latest)
		if err != nil {
			watcher.close()
			return Config{}, nil, err
		}
		current = latest
	}

	return settings, &Observer{path: path, handled: current, watcher: watcher}, nil
}

func (self *Observer) Changes() <-chan error {
	if self == nil {
		return nil
	}
	return self.watcher.events
}

func (self *Observer) Reload(watchFailure error, registry segment.Registry) ReloadResult {
	settings, changed, err := self.refresh(watchFailure)
	if err == nil && changed {
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

	current := readSnapshot(self.path)
	if current.equal(self.handled) {
		return Config{}, false, nil
	}
	self.handled = current

	settings, err := loadSnapshot(self.path, current)
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
	path string

	inotifyDescriptor int
	cancelRead        int
	cancelWrite       int
	watchDescriptor   int

	events chan error
	closed chan struct{}
	once   sync.Once
}

func newFileWatcher(path string) (*fileWatcher, error) {
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
		path:              path,
		inotifyDescriptor: inotifyDescriptor,
		cancelRead:        cancel[0],
		cancelWrite:       cancel[1],
		watchDescriptor:   -1,
		events:            make(chan error, 1),
		closed:            make(chan struct{}),
	}
	if err := watcher.arm(); err != nil {
		watcher.closeDescriptors()
		return nil, err
	}

	go watcher.run()
	return watcher, nil
}

func (self *fileWatcher) arm() error {
	directory, err := deepestExistingDirectory(filepath.Dir(self.path))
	if err != nil {
		return err
	}
	descriptor, err := unix.InotifyAddWatch(self.inotifyDescriptor, directory, watchMask)
	if err != nil {
		return err
	}
	if self.watchDescriptor >= 0 && self.watchDescriptor != descriptor {
		//nolint:gosec // inotify returns its watch descriptors as non-negative ints
		_, _ = unix.InotifyRmWatch(self.inotifyDescriptor, uint32(self.watchDescriptor))
	}
	self.watchDescriptor = descriptor
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
	defer close(self.closed)
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
		<-self.closed
	})
}

func (self *fileWatcher) closeDescriptors() {
	if self.watchDescriptor >= 0 {
		//nolint:gosec // inotify returns its watch descriptors as non-negative ints
		_, _ = unix.InotifyRmWatch(self.inotifyDescriptor, uint32(self.watchDescriptor))
	}
	_ = unix.Close(self.inotifyDescriptor)
	_ = unix.Close(self.cancelRead)
	_ = unix.Close(self.cancelWrite)
}
