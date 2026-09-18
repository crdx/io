package util

import (
	"errors"
	"path"
	"path/filepath"
	"strings"
)

func ValidateNameGlob(pattern string) error {
	if pattern == "" {
		return errors.New("the pattern is empty")
	}
	if strings.ContainsAny(pattern, `/\\`) {
		return errors.New("the pattern names a path rather than a file or directory")
	}
	_, err := path.Match(pattern, "")
	return err
}

func MatchNameGlobs(patterns []string, name string) bool {
	name = filepath.Clean(name)
	for {
		base := filepath.Base(name)
		for _, pattern := range patterns {
			if matches, _ := path.Match(pattern, base); matches {
				return true
			}
		}
		parent := filepath.Dir(name)
		if parent == name {
			return false
		}
		name = parent
	}
}
