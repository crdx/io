package main

import (
	"os"
	"path/filepath"
	"strings"
)

func goModuleCache() string {
	path := os.Getenv("GOMODCACHE")

	if path == "" {
		path = os.Getenv("GOPATH")
		if before, _, found := strings.Cut(path, string(os.PathListSeparator)); found {
			path = before
		}

		if path == "" {
			if home, err := os.UserHomeDir(); err == nil {
				path = filepath.Join(home, "go")
			}
		}

		if path != "" {
			path = filepath.Join(path, "pkg", "mod")
		}
	}

	if path != "" && !filepath.IsAbs(path) {
		var err error
		path, err = filepath.Abs(path)
		if err != nil {
			return ""
		}
	}

	path = filepath.Clean(path)

	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return path
	}

	return ""
}
