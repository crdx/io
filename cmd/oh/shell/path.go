package shell

import (
	"os"
	"path/filepath"

	"crdx.org/io/internal/pathutil"
)

// Paths are the additional host paths made available to shell and file tools.
type Paths struct {
	Read  []string `toml:"read"`
	Write []string `toml:"write"`
	Exec  []string `toml:"exec"`
	Home  []string `toml:"home"`
}

// HomeRelativePath returns the path relative to the host home directory.
func HomeRelativePath(path string) (string, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}

	return pathutil.RelativeTo(home, path)
}

func miseDataDir() string {
	if dataDir := os.Getenv("MISE_DATA_DIR"); dataDir != "" {
		return dataDir
	}

	if dataHome := os.Getenv("XDG_DATA_HOME"); filepath.IsAbs(dataHome) {
		return filepath.Join(dataHome, "mise")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	return filepath.Join(home, ".local", "share", "mise")
}
