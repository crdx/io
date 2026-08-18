// Package util provides small helpers shared across io.
package util

import "crdx.org/io/internal/file"

// RootName turns a tool-supplied path into the relative name Root's own methods take. It rejects a
// name that plainly leaves the root; Root re-validates the name when it is opened.
func RootName(root *file.Root, path string) (string, error) {
	resolvedRoot, name, err := root.Resolve(path)
	if err != nil {
		return "", err
	}
	if resolvedRoot != root {
		return "", file.ErrReadOnly
	}

	return name, nil
}
