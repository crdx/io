package file

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"

	"crdx.org/io/internal/util/pathutil"
)

var ErrReadOnly = errors.New("the filesystem is read-only")

var ErrGitDir = errors.New("refusing to touch anything inside a .git directory")

var ErrOutsideRoot = errors.New("path is outside the root")

func InGitDir(name string) bool {
	segments := strings.Split(filepath.Clean(name), string(filepath.Separator))

	return slices.Contains(segments, ".git")
}

func RefuseGitDir(name string) error {
	if InGitDir(name) {
		return ErrGitDir
	}

	return nil
}

type mountedRoot struct {
	root    *Root
	name    string
	isExact bool
}

type Root struct {
	root   *os.Root
	refuse func(name string) error

	mountsMutex sync.RWMutex
	mounts      map[string]mountedRoot
}

func New(root *os.Root, refuseWrite func(name string) error) *Root {
	return &Root{root: root, refuse: refuseWrite, mounts: map[string]mountedRoot{}}
}

func (self *Root) Mount(path string, root *Root) {
	self.mountsMutex.Lock()
	defer self.mountsMutex.Unlock()
	self.mounts[filepath.Clean(path)] = mountedRoot{root: root, name: "."}
}

func (self *Root) MountFile(path string, root *Root, name string) {
	self.mountsMutex.Lock()
	defer self.mountsMutex.Unlock()
	self.mounts[filepath.Clean(path)] = mountedRoot{root: root, name: name, isExact: true}
}

func (self *Root) Unmount(path string) {
	self.mountsMutex.Lock()
	defer self.mountsMutex.Unlock()
	delete(self.mounts, filepath.Clean(path))
}

func (self *Root) Resolve(path string) (*Root, string, error) {
	if path == "" {
		return self, ".", nil
	}
	if !filepath.IsAbs(path) {
		if !filepath.IsLocal(path) {
			return nil, "", ErrOutsideRoot
		}

		return self, path, nil
	}

	if name, ok := pathutil.RelativeTo(self.Name(), path); ok {
		return self, name, nil
	}

	self.mountsMutex.RLock()
	defer self.mountsMutex.RUnlock()

	var resolvedRoot *Root
	resolvedName := ""
	resolvedAt := ""
	for at, mountedRoot := range self.mounts {
		name, below := pathutil.RelativeTo(at, path)
		if !below || len(at) <= len(resolvedAt) || (mountedRoot.isExact && name != ".") {
			continue
		}

		resolvedRoot = mountedRoot.root
		resolvedName = filepath.Join(mountedRoot.name, name)
		resolvedAt = at
	}
	if resolvedRoot != nil {
		return resolvedRoot, resolvedName, nil
	}

	return nil, "", ErrOutsideRoot
}

func (self *Root) RefuseWrite(name string) error { return self.refuse(name) }

func (self *Root) Name() string { return self.root.Name() }

func (self *Root) FS() fs.FS { return self.root.FS() }

func (self *Root) Open(name string) (*os.File, error) { return self.root.Open(name) }

func (self *Root) ReadFile(name string) ([]byte, error) { return self.root.ReadFile(name) }

func (self *Root) Stat(name string) (os.FileInfo, error) { return self.root.Stat(name) }

func (self *Root) WriteFile(name string, data []byte, perm os.FileMode) error {
	if err := self.refuseWrite(name); err != nil {
		return err
	}

	return self.root.WriteFile(name, data, perm)
}

func (self *Root) MkdirAll(name string, perm os.FileMode) error {
	if err := self.refuseWrite(name); err != nil {
		return err
	}

	return self.root.MkdirAll(name, perm)
}

func (self *Root) refuseWrite(name string) error {
	if err := self.refuse(name); err != nil {
		return err
	}

	resolvedName := filepath.Clean(name)

	for range 255 {
		parts := strings.Split(resolvedName, string(filepath.Separator))
		didFollowSymlink := false

		for i := range parts {
			prefix := filepath.Join(parts[:i+1]...)
			info, err := self.root.Lstat(prefix)
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return nil
				}
				return err
			}
			if info.Mode()&os.ModeSymlink == 0 {
				continue
			}

			target, err := self.root.Readlink(prefix)
			if err != nil {
				return err
			}

			remainingPath := strings.Join(parts[i+1:], string(filepath.Separator))
			resolvedName = filepath.Clean(filepath.Join(filepath.Dir(prefix), target, remainingPath))
			if err := self.refuse(resolvedName); err != nil {
				return err
			}

			didFollowSymlink = true
			break
		}

		if !didFollowSymlink {
			return nil
		}
	}

	return &os.PathError{Op: "write", Path: name, Err: syscall.ELOOP}
}
