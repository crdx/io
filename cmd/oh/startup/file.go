package startup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const messageIntroduction = "The following files have been made available in this session's scratch directory:"

func PrepareInitialFiles(sourcePaths []string, scratchDirectory string) (string, error) {
	destinationPaths := make([]string, 0, len(sourcePaths))
	for _, sourcePath := range sourcePaths {
		content, err := os.ReadFile(sourcePath) //nolint:gosec // the path names a file selected by --add
		if err != nil {
			return "", fmt.Errorf("could not read the initial file: %w", err)
		}

		destinationPath := filepath.Join(scratchDirectory, filepath.Base(sourcePath))
		if err := os.WriteFile(destinationPath, content, 0o600); err != nil { //nolint:gosec // a generated scratch directory receives the source basename
			return "", fmt.Errorf("could not copy the initial file: %w", err)
		}
		destinationPaths = append(destinationPaths, destinationPath)
	}

	listedPaths := make([]string, len(destinationPaths))
	for i, destinationPath := range destinationPaths {
		listedPaths[i] = "- " + destinationPath
	}
	return messageIntroduction + "\n\n" + strings.Join(listedPaths, "\n"), nil
}
