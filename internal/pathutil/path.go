// Package pathutil provides small filesystem path helpers.
package pathutil

import (
	"os"
	"path/filepath"
)

// Abbr returns the final element of path.
func Abbr(path string) string {
	return filepath.Base(path)
}

// Exists reports whether path can be statted.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// RelativeTo returns path relative to root when path is within root.
func RelativeTo(root string, path string) (string, bool) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", false
	}

	name, err := filepath.Rel(root, path)
	return name, err == nil && filepath.IsLocal(name)
}
