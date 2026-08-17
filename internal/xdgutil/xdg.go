// Package xdgutil resolves paths in XDG base directories.
package xdgutil

import (
	"os"
	"path/filepath"
)

// StatePath returns a path below the XDG state directory.
func StatePath(parts ...string) string {
	root := os.Getenv("XDG_STATE_HOME")

	if !filepath.IsAbs(root) {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}

		root = filepath.Join(home, ".local", "state")
	}

	return filepath.Join(append([]string{root}, parts...)...)
}
