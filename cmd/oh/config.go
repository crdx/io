package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"

	"crdx.org/io/internal/pathutil"
)

type configuredSettings struct {
	Model  string `toml:"model"`
	Effort string `toml:"effort"`

	Skill struct {
		LookupDirectories []string `toml:"lookup_dirs"`
	} `toml:"skill"`
	Sandbox configuredPaths `toml:"sandbox"`
}

type configuredPaths struct {
	Read  []string `toml:"read"`
	Write []string `toml:"write"`
	Exec  []string `toml:"exec"`
}

func loadConfiguredSettings(path string) (configuredSettings, error) {
	var settings configuredSettings
	if path == "" {
		return settings, nil
	}

	shownPath := pathutil.Shorten(path)

	metadata, err := toml.DecodeFile(path, &settings)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return settings, nil
	case err != nil:
		return settings, fmt.Errorf("%s: %w", shownPath, err)
	}

	if metadata.IsDefined("model") && settings.Model == "" {
		return settings, fmt.Errorf("%s: model is empty, so there is nothing to ask", shownPath)
	}
	if metadata.IsDefined("effort") && settings.Effort == "" {
		return settings, fmt.Errorf("%s: effort is empty, so there is nothing to ask for", shownPath)
	}

	lists := []struct {
		name   string
		values *[]string
	}{
		{"skill.lookup_dirs", &settings.Skill.LookupDirectories},
		{"sandbox.read", &settings.Sandbox.Read},
		{"sandbox.write", &settings.Sandbox.Write},
		{"sandbox.exec", &settings.Sandbox.Exec},
	}
	for _, list := range lists {
		for index, written := range *list.values {
			resolved, err := resolveConfiguredPath(path, written)
			if err != nil {
				return settings, fmt.Errorf("%s: %s: %w", shownPath, list.name, err)
			}
			(*list.values)[index] = resolved
		}
	}

	return settings, nil
}

func resolveConfiguredPath(configurationPath, written string) (string, error) {
	if written == "" {
		return "", errors.New("path is empty")
	}

	path := written
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("could not expand %q: %w", written, err)
		}
		path = home
		if written != "~" {
			path = filepath.Join(home, strings.TrimPrefix(written, "~/"))
		}
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(filepath.Dir(configurationPath), path)
	}

	return filepath.Clean(path), nil
}
