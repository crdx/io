package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/internal/file"
	"crdx.org/io/internal/pathutil"
	"crdx.org/io/internal/util"
)

type configuredMount struct {
	root    *os.Root
	target  string
	name    string
	isExact bool
}

func createMissingConfiguredPaths(paths configuredPaths, warnings io.Writer) (configuredPaths, error) {
	filtered := configuredPaths{}
	lists := []struct {
		source []string
		target *[]string
	}{
		{paths.Read, &filtered.Read},
		{paths.Write, &filtered.Write},
		{paths.Exec, &filtered.Exec},
		{paths.Home, &filtered.Home},
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
				return configuredPaths{}, fmt.Errorf(
					"could not mount configured path %s: %w",
					pathutil.Shorten(path),
					err,
				)
			}
			*list.target = append(*list.target, path)
		}
	}

	return filtered, nil
}

func mountConfiguredPaths(files *file.Root, mode *caps.Mode, extraPaths configuredPaths) ([]*os.Root, error) {
	writable := make(map[string]bool, len(extraPaths.Read)+len(extraPaths.Write))
	for _, path := range extraPaths.Read {
		writable[path] = false
	}
	for _, path := range extraPaths.Write {
		writable[path] = true
	}

	paths := make([]string, 0, len(writable))
	for path := range writable {
		paths = append(paths, path)
	}
	slices.Sort(paths)

	opened := make([]*os.Root, 0, len(paths))
	for _, path := range paths {
		mount, err := openConfiguredMount(path)
		if err != nil {
			closeConfiguredRoots(opened)
			return nil, fmt.Errorf("could not mount configured path %s: %w", pathutil.Shorten(path), err)
		}

		newRefusal := func(string) error { return file.ErrReadOnly }
		if writable[path] {
			currentRefusal := caps.RefuseWrite(mode)
			newRefusal = func(name string) error {
				if mount.isExact {
					return currentRefusal(mount.target)
				}
				return currentRefusal(filepath.Join(mount.target, name))
			}
		}

		mountedRoot := file.New(mount.root, newRefusal)
		if mount.isExact {
			files.MountFile(path, mountedRoot, mount.name)
		} else {
			files.Mount(path, mountedRoot)
		}
		opened = append(opened, mount.root)
	}

	return opened, nil
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

func closeConfiguredRoots(roots []*os.Root) {
	for _, root := range roots {
		_ = root.Close()
	}
}
