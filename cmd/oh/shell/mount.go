package shell

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/internal/file"
	"crdx.org/io/internal/util"
	"crdx.org/io/internal/util/pathutil"
)

type configuredMount struct {
	root    *os.Root
	target  string
	name    string
	isExact bool
}

type mountedPath struct {
	mount *configuredMount
	root  *file.Root
}

type PathAccess struct {
	files *file.Root
	mode  *caps.Mode

	mutex             sync.RWMutex
	configuredPaths   Paths
	configuredMounts  map[string]mountedPath
	temporaryMounts   map[string]mountedPath
	temporaryWritable map[string]bool
	roots             []*os.Root
}

func NewPathAccess(files *file.Root, mode *caps.Mode, paths Paths) (*PathAccess, error) {
	access := &PathAccess{
		files:             files,
		mode:              mode,
		configuredPaths:   clonePaths(paths),
		configuredMounts:  make(map[string]mountedPath),
		temporaryMounts:   make(map[string]mountedPath),
		temporaryWritable: make(map[string]bool),
	}

	for _, path := range sortedPathModes(paths) {
		configuredPathMount, err := access.openMountedPath(path.path, path.isWritable)
		if err != nil {
			access.Close()
			return nil, fmt.Errorf("could not mount configured path %s: %w", pathutil.Shorten(path.path), err)
		}
		access.configuredMounts[path.path] = configuredPathMount
		access.install(path.path, configuredPathMount)
	}

	return access, nil
}

func (self *PathAccess) GetPaths() Paths {
	self.mutex.RLock()
	defer self.mutex.RUnlock()

	paths := clonePaths(self.configuredPaths)
	for _, path := range slices.Sorted(maps.Keys(self.temporaryWritable)) {
		if self.temporaryWritable[path] {
			paths.Write = append(paths.Write, path)
		} else {
			paths.Read = append(paths.Read, path)
		}
	}
	return paths
}

func (self *PathAccess) Grant(path string, isWritable bool) (bool, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	path = filepath.Clean(path)
	if current, exists := self.temporaryWritable[path]; exists && current == isWritable {
		return false, nil
	}

	temporaryPathMount, err := self.openMountedPath(path, isWritable)
	if err != nil {
		return false, err
	}
	previousMount, hasPreviousMount := self.temporaryMounts[path]
	self.temporaryMounts[path] = temporaryPathMount
	self.temporaryWritable[path] = isWritable
	self.install(path, temporaryPathMount)
	if hasPreviousMount {
		_ = previousMount.mount.root.Close()
	}
	return true, nil
}

func (self *PathAccess) Revoke(path string) bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	path = filepath.Clean(path)
	if _, exists := self.temporaryWritable[path]; !exists {
		return false
	}
	temporaryPathMount := self.temporaryMounts[path]
	delete(self.temporaryMounts, path)
	delete(self.temporaryWritable, path)

	if configuredPathMount, exists := self.configuredMounts[path]; exists {
		self.install(path, configuredPathMount)
	} else {
		self.files.Unmount(path)
	}
	_ = temporaryPathMount.mount.root.Close()
	return true
}

func (self *PathAccess) Close() {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	closeRoots(self.roots)
	self.roots = nil
}

func (self *PathAccess) openMountedPath(path string, isWritable bool) (mountedPath, error) {
	mount, err := openConfiguredMount(path)
	if err != nil {
		return mountedPath{}, err
	}
	self.roots = append(self.roots, mount.root)
	return mountedPath{mount: &mount, root: newMountedRoot(self.mode, mount, isWritable)}, nil
}

func (self *PathAccess) install(path string, pathMount mountedPath) {
	if pathMount.mount.isExact {
		self.files.MountFile(path, pathMount.root, pathMount.mount.name)
	} else {
		self.files.Mount(path, pathMount.root)
	}
}

func clonePaths(paths Paths) Paths {
	return Paths{
		Read:  slices.Clone(paths.Read),
		Write: slices.Clone(paths.Write),
		Exec:  slices.Clone(paths.Exec),
		Home:  slices.Clone(paths.Home),
	}
}

// PreparePaths creates configured paths that do not yet exist and omits any that cannot be created.
func PreparePaths(paths Paths, warnings io.Writer) (Paths, error) {
	filteredPaths := Paths{}
	lists := []struct {
		source []string
		target *[]string
	}{
		{paths.Read, &filteredPaths.Read},
		{paths.Write, &filteredPaths.Write},
		{paths.Exec, &filteredPaths.Exec},
		{paths.Home, &filteredPaths.Home},
	}

	for _, list := range lists {
		for _, path := range list.source {
			_, err := os.Stat(path)
			if errors.Is(err, fs.ErrNotExist) {
				if err := os.MkdirAll(path, 0o700); err != nil {
					util.WriteWarningf(
						warnings,
						"could not create configured path %s: %v",
						pathutil.Shorten(path),
						err,
					)
					continue
				}
			} else if err != nil {
				return Paths{}, fmt.Errorf(
					"could not mount configured path %s: %w",
					pathutil.Shorten(path),
					err,
				)
			}
			*list.target = append(*list.target, path)
		}
	}

	return filteredPaths, nil
}

type pathMode struct {
	path       string
	isWritable bool
}

func sortedPathModes(paths Paths) []pathMode {
	writable := make(map[string]bool, len(paths.Read)+len(paths.Write))
	for _, path := range paths.Read {
		writable[filepath.Clean(path)] = false
	}
	for _, path := range paths.Write {
		writable[filepath.Clean(path)] = true
	}

	names := slices.Sorted(maps.Keys(writable))
	modes := make([]pathMode, 0, len(names))
	for _, path := range names {
		modes = append(modes, pathMode{path: path, isWritable: writable[path]})
	}
	return modes
}

func newMountedRoot(mode *caps.Mode, mount configuredMount, isWritable bool) *file.Root {
	refuseWrite := func(string) error { return file.ErrReadOnly }
	if isWritable {
		currentRefusal := caps.RefuseWrite(mode)
		refuseWrite = func(name string) error {
			if mount.isExact {
				return currentRefusal(mount.target)
			}
			return currentRefusal(filepath.Join(mount.target, name))
		}
	}
	return file.New(mount.root, refuseWrite)
}

func openConfiguredMount(path string) (configuredMount, error) {
	info, err := os.Stat(path)
	if err != nil {
		return configuredMount{}, err
	}

	if info.IsDir() {
		root, err := os.OpenRoot(path)
		return configuredMount{root: root, target: path, name: "."}, err
	}

	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return configuredMount{}, err
	}
	root, err := os.OpenRoot(filepath.Dir(target))
	return configuredMount{
		root:    root,
		target:  target,
		name:    filepath.Base(target),
		isExact: true,
	}, err
}

func closeRoots(roots []*os.Root) {
	for _, root := range roots {
		_ = root.Close()
	}
}
