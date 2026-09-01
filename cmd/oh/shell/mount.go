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

	mutex            sync.RWMutex
	configuredPaths  Paths
	configuredMounts map[string]mountedPath
	temporaryMounts  map[string]mountedPath
	temporaryAccess  map[string]Access
	roots            []*os.Root
}

func NewPathAccess(files *file.Root, mode *caps.Mode, paths Paths) (*PathAccess, error) {
	access := &PathAccess{
		files:            files,
		mode:             mode,
		configuredPaths:  clonePaths(paths),
		configuredMounts: make(map[string]mountedPath),
		temporaryMounts:  make(map[string]mountedPath),
		temporaryAccess:  make(map[string]Access),
	}

	for _, path := range sortedPathModes(paths) {
		configuredPathMount, err := access.openMountedPath(path.path, path.access)
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
	for _, path := range slices.Sorted(maps.Keys(self.temporaryAccess)) {
		access := self.temporaryAccess[path]
		switch {
		case access.Has(WriteAccess):
			paths.Write = append(paths.Write, path)
		default:
			paths.Read = append(paths.Read, path)
		}
		if access.Has(ExecAccess) {
			paths.Exec = append(paths.Exec, path)
		}
	}
	return paths
}

func (self *PathAccess) Grant(path string, access Access) (bool, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	path = filepath.Clean(path)
	if current, exists := self.temporaryAccess[path]; exists && current == access {
		return false, nil
	}

	temporaryPathMount, err := self.openMountedPath(path, access)
	if err != nil {
		return false, err
	}
	self.releaseTemporaryMount(path)
	self.temporaryMounts[path] = temporaryPathMount
	self.temporaryAccess[path] = access
	self.install(path, temporaryPathMount)
	return true, nil
}

func (self *PathAccess) Revoke(path string) bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	path = filepath.Clean(path)
	if _, exists := self.temporaryAccess[path]; !exists {
		return false
	}
	delete(self.temporaryAccess, path)
	self.releaseTemporaryMount(path)
	return true
}

func (self *PathAccess) Close() {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	closeRoots(self.roots)
	self.roots = nil
}

func (self *PathAccess) releaseTemporaryMount(path string) {
	temporaryPathMount, exists := self.temporaryMounts[path]
	if !exists {
		return
	}
	delete(self.temporaryMounts, path)

	if configuredPathMount, exists := self.configuredMounts[path]; exists {
		self.install(path, configuredPathMount)
	} else {
		self.files.Unmount(path)
	}
	_ = temporaryPathMount.mount.root.Close()
}

func (self *PathAccess) openMountedPath(path string, access Access) (mountedPath, error) {
	mount, err := openConfiguredMount(path)
	if err != nil {
		return mountedPath{}, err
	}
	self.roots = append(self.roots, mount.root)
	return mountedPath{mount: &mount, root: newMountedRoot(self.mode, mount, access)}, nil
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
	path   string
	access Access
}

func sortedPathModes(paths Paths) []pathMode {
	accessByPath := make(map[string]Access, len(paths.Read)+len(paths.Write)+len(paths.Exec))
	for _, list := range []struct {
		paths  []string
		access Access
	}{
		{paths.Read, ReadAccess},
		{paths.Write, ReadAccess | WriteAccess},
		{paths.Exec, ReadAccess | ExecAccess},
	} {
		for _, path := range list.paths {
			accessByPath[filepath.Clean(path)] |= list.access
		}
	}

	names := slices.Sorted(maps.Keys(accessByPath))
	modes := make([]pathMode, 0, len(names))
	for _, path := range names {
		modes = append(modes, pathMode{path: path, access: accessByPath[path]})
	}
	return modes
}

func newMountedRoot(mode *caps.Mode, mount configuredMount, access Access) *file.Root {
	refuseWrite := func(string) error { return file.ErrReadOnly }
	if access.Has(WriteAccess) {
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
