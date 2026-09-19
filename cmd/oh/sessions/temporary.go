package sessions

import (
	"fmt"
	"os"

	"crdx.org/io/cmd/oh/location"
)

const ephemeralDirectoryPrefix = "oh-"

func PrepareTemporaryDirectory(name string) (string, error) {
	temporaryDirectory := location.GetTmpDir(name)

	if err := os.MkdirAll(temporaryDirectory, 0o700); err != nil {
		return "", fmt.Errorf("could not prepare the tmp dir: %w", err)
	}

	return temporaryDirectory, nil
}

func PrepareEphemeralDirectory() (string, error) {
	directory, err := os.MkdirTemp("", ephemeralDirectoryPrefix)
	if err != nil {
		return "", fmt.Errorf("could not prepare the temporary scratch: %w", err)
	}
	return directory, nil
}
