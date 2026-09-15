package shell

import (
	"os"
	"path/filepath"
	"strings"

	"crdx.org/io/internal/util/pathutil"
)

type Paths struct {
	Read  []string `toml:"read"`
	Write []string `toml:"write"`
	Exec  []string `toml:"exec"`
	Path  []string `toml:"path"`
	Home  []string `toml:"home"`
}

func ShellPath(pathDirectories []string) string {
	return strings.Join(append([]string{os.Getenv("PATH")}, pathDirectories...), string(os.PathListSeparator))
}

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
