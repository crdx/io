package pathutil

import (
	"os"
	"path/filepath"
	"strings"
)

const separator = string(os.PathSeparator)

func Shorten(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}

	home = strings.TrimSuffix(home, separator)
	if home == "" {
		return path
	}

	if path == home {
		return "~"
	}

	if rest, below := strings.CutPrefix(path, home+separator); below {
		return "~" + separator + rest
	}

	return path
}

func Expand(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~"+separator) {
		return path, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	if path == "~" {
		return home, nil
	}

	return filepath.Join(home, strings.TrimPrefix(path, "~"+separator)), nil
}

func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func RelativeTo(root string, path string) (string, bool) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", false
	}

	name, err := filepath.Rel(root, path)
	return name, err == nil && filepath.IsLocal(name)
}
