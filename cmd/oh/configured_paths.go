package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"crdx.org/io/internal/file"
	"crdx.org/io/internal/pathutil"
)

func mountConfiguredPaths(files *file.Root, mode *Mode, extraPaths configuredPaths) ([]*os.Root, error) {
	writable := make(map[string]bool, len(extraPaths.Read)+len(extraPaths.Write))
	for _, path := range extraPaths.Read {
		writable[path] = false
	}
	for _, path := range extraPaths.Write {
		writable[path] = true
	}

	directories := make([]string, 0, len(writable))
	for directory := range writable {
		directories = append(directories, directory)
	}
	slices.Sort(directories)

	opened := make([]*os.Root, 0, len(directories))
	for _, directory := range directories {
		root, err := os.OpenRoot(directory)
		if err != nil {
			closeConfiguredRoots(opened)
			return nil, fmt.Errorf("could not mount configured path %s: %w", pathutil.Shorten(directory), err)
		}

		refuse := func(string) error { return file.ErrReadOnly }
		if writable[directory] {
			currentRefusal := refuseWrite(mode)
			refuse = func(name string) error {
				return currentRefusal(filepath.Join(directory, name))
			}
		}
		files.Mount(directory, file.New(root, refuse))
		opened = append(opened, root)
	}

	return opened, nil
}

func closeConfiguredRoots(roots []*os.Root) {
	for _, root := range roots {
		_ = root.Close()
	}
}
