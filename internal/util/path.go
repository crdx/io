// Package util is the plumbing the file tools share: paths, walking, globs, and search results.
package util

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// ErrGitDir refuses a write or an edit inside a repository's own metadata.
var ErrGitDir = errors.New("refusing to touch anything inside a .git directory")

// RootName turns a tool-supplied path into the relative name Root's own methods take. It checks
// nothing: Root re-validates the name when it is opened.
func RootName(root *os.Root, path string) (string, error) {
	if path == "" {
		return ".", nil
	}

	if !filepath.IsAbs(path) {
		return path, nil
	}

	return filepath.Rel(root.Name(), path)
}

// WithinGitDir reports whether name has a ".git" path component, which Root's confinement allows.
func WithinGitDir(name string) bool {
	segments := strings.Split(filepath.Clean(name), string(filepath.Separator))
	return slices.Contains(segments, ".git")
}
