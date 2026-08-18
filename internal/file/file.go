// Package file confines file tools to a root and applies a caller-supplied write policy.
package file

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"crdx.org/io/internal/pathutil"
)

// ErrReadOnly is what a call that would change something gets while writing is withheld.
var ErrReadOnly = errors.New("the filesystem is read-only")

// ErrGitDir refuses a change inside a repository's own metadata.
var ErrGitDir = errors.New("refusing to touch anything inside a .git directory")

// ErrOutsideRoot is a path resolving to somewhere the root does not reach.
var ErrOutsideRoot = errors.New("path is outside the root")

// InGitDir reports whether a path contains a .git component.
func InGitDir(name string) bool {
	segments := strings.Split(filepath.Clean(name), string(filepath.Separator))

	return slices.Contains(segments, ".git")
}

// RefuseGitDir returns ErrGitDir for paths containing a .git component.
func RefuseGitDir(name string) error {
	if InGitDir(name) {
		return ErrGitDir
	}

	return nil
}

// Root is a directory the tools are confined to, and a rule about what may be changed within it.
type Root struct {
	root   *os.Root                // the confinement itself
	refuse func(name string) error // what stands in the way of changing a path, asked afresh
	mounts map[string]*Root        // other trees by absolute name
}

// New builds a Root over an open directory. refuse is checked before each change.
func New(root *os.Root, refuseWrite func(name string) error) *Root {
	return &Root{root: root, refuse: refuseWrite, mounts: map[string]*Root{}}
}

// Mount adds a tree at an absolute path.
func (self *Root) Mount(path string, root *Root) {
	self.mounts[filepath.Clean(path)] = root
}

// Resolve finds the tree and local name for a path.
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

	var resolvedRoot *Root
	resolvedName := ""
	resolvedAt := ""
	for at, mountedRoot := range self.mounts {
		if name, ok := pathutil.RelativeTo(at, path); ok && len(at) > len(resolvedAt) {
			resolvedRoot = mountedRoot
			resolvedName = name
			resolvedAt = at
		}
	}
	if resolvedRoot != nil {
		return resolvedRoot, resolvedName, nil
	}

	return nil, "", ErrOutsideRoot
}

// RefuseWrite returns the current refusal for a path.
func (self *Root) RefuseWrite(name string) error { return self.refuse(name) }

// Name is the directory the tools are confined to.
func (self *Root) Name() string { return self.root.Name() }

// FS is the tree as a filesystem, for walking and reading.
func (self *Root) FS() fs.FS { return self.root.FS() }

// Open opens a file or a directory for reading.
func (self *Root) Open(name string) (*os.File, error) { return self.root.Open(name) }

// ReadFile reads a whole file.
func (self *Root) ReadFile(name string) ([]byte, error) { return self.root.ReadFile(name) }

// Stat is what is known about a file without opening it.
func (self *Root) Stat(name string) (os.FileInfo, error) { return self.root.Stat(name) }

// WriteFile writes a whole file, where the rule allows it.
func (self *Root) WriteFile(name string, data []byte, perm os.FileMode) error {
	if err := self.refuseWrite(name); err != nil {
		return err
	}

	return self.root.WriteFile(name, data, perm)
}

// MkdirAll makes a directory and every parent it needs, where the rule allows it.
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

	for range 255 { // the kernel gives up on a symlink chain long before this
		parts := strings.Split(resolvedName, string(filepath.Separator))
		followedSymlink := false

		for index := range parts {
			prefix := filepath.Join(parts[:index+1]...)
			info, err := self.root.Lstat(prefix)
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return nil // the rest is to be made, and contains no existing symlink
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

			remainingPath := strings.Join(parts[index+1:], string(filepath.Separator))
			resolvedName = filepath.Clean(filepath.Join(filepath.Dir(prefix), target, remainingPath))
			if err := self.refuse(resolvedName); err != nil {
				return err
			}

			followedSymlink = true
			break
		}

		if !followedSymlink {
			return nil
		}
	}

	return &os.PathError{Op: "write", Path: name, Err: syscall.ELOOP}
}
