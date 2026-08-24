// Package pathutil provides small filesystem path helpers.
package pathutil

import (
	"os"
	"path/filepath"
	"strings"
)

const separator = string(os.PathSeparator)

// Shorten ensures a path is as compact as it can be.
func Shorten(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}

	home = strings.TrimSuffix(home, separator)
	if home == "" {
		return path
	}

	if path == home {
		return "~"
	}

	if rest, below := strings.CutPrefix(path, home+separator); below {
		return "~" + separator + rest
	}

	return path
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
