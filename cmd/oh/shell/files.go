package shell

import (
	"fmt"
	"os"

	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/internal/file"
	"crdx.org/io/internal/sandbox"
)

// MountHomeDirectory exposes the shell home through the file tools.
func MountHomeDirectory(files *file.Root, homeDirectory string, mode *caps.Mode) (*os.Root, error) {
	homeRoot, err := os.OpenRoot(homeDirectory)
	if err != nil {
		return nil, fmt.Errorf("could not open the shell home: %w", err)
	}

	files.Mount(homeDirectory, file.New(homeRoot, caps.RefuseWrite(mode)))
	return homeRoot, nil
}

// MountTemporaryDirectory exposes the session scratch directory as /tmp through the file tools.
func MountTemporaryDirectory(files *file.Root, temporaryDirectory string) (*os.Root, error) {
	temporaryRoot, err := os.OpenRoot(temporaryDirectory)
	if err != nil {
		return nil, fmt.Errorf("could not open the tmp dir: %w", err)
	}

	files.Mount(sandbox.TmpDir, file.New(temporaryRoot, func(string) error { return nil }))
	return temporaryRoot, nil
}
