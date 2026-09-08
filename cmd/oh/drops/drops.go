package drops

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"crdx.org/io/internal/file"
)

const directoryName = "drops"

func GetDirectory(sessionDirectory string) string {
	return filepath.Join(sessionDirectory, directoryName)
}

func Mount(files *file.Root, sessionDirectory string) (func() error, bool, error) {
	directory := GetDirectory(sessionDirectory)
	root, err := os.OpenRoot(directory)
	if errors.Is(err, fs.ErrNotExist) {
		return func() error { return nil }, false, nil
	}
	if err != nil {
		return func() error { return nil }, false, err
	}

	files.Mount(directory, file.New(root, func(string) error { return file.ErrReadOnly }))
	return root.Close, true, nil
}
