package drops

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"crdx.org/io/internal/file"
)

const directoryName = "drops"

func GetDirectory(sessionDirectory string) string {
	return filepath.Join(sessionDirectory, directoryName)
}

func Prepare(sessionDirectory string, ensureSession func() error) (string, error) {
	if err := ensureSession(); err != nil {
		return "", fmt.Errorf("prepare the session directory: %w", err)
	}
	sessionRoot, err := os.OpenRoot(sessionDirectory)
	if err != nil {
		return "", fmt.Errorf("open the session directory: %w", err)
	}
	defer func() { _ = sessionRoot.Close() }()

	info, err := sessionRoot.Lstat(directoryName)
	if errors.Is(err, fs.ErrNotExist) {
		if err := sessionRoot.Mkdir(directoryName, 0o700); err != nil {
			return "", fmt.Errorf("prepare the drops directory: %w", err)
		}
	} else if err != nil {
		return "", fmt.Errorf("inspect the drops directory: %w", err)
	} else if err := validateDirectory(info); err != nil {
		return "", err
	}
	return GetDirectory(sessionDirectory), nil
}

func CopyFile(sessionDirectory string, ensureSession func() error, sourcePath string, fileName string) (string, error) {
	if !filepath.IsLocal(fileName) || fileName != filepath.Base(fileName) {
		return "", fmt.Errorf("drop file name %q is not a base name", fileName)
	}

	sourceRoot, err := os.OpenRoot(filepath.Dir(sourcePath))
	if err != nil {
		return "", fmt.Errorf("open the source directory: %w", err)
	}
	defer func() { _ = sourceRoot.Close() }()
	source, err := sourceRoot.Open(filepath.Base(sourcePath))
	if err != nil {
		return "", fmt.Errorf("open the source file: %w", err)
	}
	defer func() { _ = source.Close() }()

	directory, err := Prepare(sessionDirectory, ensureSession)
	if err != nil {
		return "", err
	}
	destinationRoot, err := os.OpenRoot(directory)
	if err != nil {
		return "", fmt.Errorf("open the drops directory: %w", err)
	}
	defer func() { _ = destinationRoot.Close() }()
	destination, err := destinationRoot.OpenFile(fileName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("create the drop: %w", err)
	}

	if _, err := io.Copy(destination, source); err != nil {
		_ = destination.Close()
		_ = destinationRoot.Remove(fileName)
		return "", fmt.Errorf("copy the drop: %w", err)
	}
	if err := destination.Close(); err != nil {
		_ = destinationRoot.Remove(fileName)
		return "", fmt.Errorf("close the drop: %w", err)
	}
	return filepath.Join(directory, fileName), nil
}

func Mount(files *file.Root, sessionDirectory string) (func() error, bool, error) {
	sessionRoot, err := os.OpenRoot(sessionDirectory)
	if errors.Is(err, fs.ErrNotExist) {
		return func() error { return nil }, false, nil
	}
	if err != nil {
		return func() error { return nil }, false, err
	}
	defer func() { _ = sessionRoot.Close() }()

	info, err := sessionRoot.Lstat(directoryName)
	if errors.Is(err, fs.ErrNotExist) {
		return func() error { return nil }, false, nil
	}
	if err != nil {
		return func() error { return nil }, false, err
	}
	if err := validateDirectory(info); err != nil {
		return func() error { return nil }, false, err
	}
	dropsRoot, err := sessionRoot.OpenRoot(directoryName)
	if err != nil {
		return func() error { return nil }, false, err
	}

	directory := GetDirectory(sessionDirectory)
	files.Mount(directory, file.New(dropsRoot, func(string) error { return file.ErrReadOnly }))
	return dropsRoot.Close, true, nil
}

func validateDirectory(info fs.FileInfo) error {
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		return errors.New("the drops path is a symbolic link")
	case !info.IsDir():
		return errors.New("the drops path is not a directory")
	default:
		return nil
	}
}
