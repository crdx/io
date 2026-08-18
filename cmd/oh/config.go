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

const defaultGetOnWithItMessage = "yes"

type configuredSettings struct {
	Model              string `toml:"model"`
	Effort             string `toml:"effort"`
	GetOnWithItMessage string `toml:"get_on_with_it_message"`

	Skill struct {
		LookupDirs []string `toml:"lookup"`
	} `toml:"skill"`
	Sandbox configuredPaths `toml:"sandbox"`
}

type configuredPaths struct {
	Read  []string `toml:"read"`
	Write []string `toml:"write"`
	Exec  []string `toml:"exec"`
}

func loadConfiguredSettings(path string) (configuredSettings, error) {
	settings := configuredSettings{GetOnWithItMessage: defaultGetOnWithItMessage}
	if path == "" {
		return settings, nil
	}

	shownPath := pathutil.Shorten(path)

	meta, err := toml.DecodeFile(path, &settings)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return settings, nil
	case err != nil:
		return settings, fmt.Errorf("%s: %w", shownPath, err)
	}

	if meta.IsDefined("model") && settings.Model == "" {
		return settings, fmt.Errorf("%s: model is empty, so there is nothing to ask", shownPath)
	}
	if meta.IsDefined("effort") && settings.Effort == "" {
		return settings, fmt.Errorf("%s: effort is empty, so there is nothing to ask for", shownPath)
	}
	settings.GetOnWithItMessage = strings.TrimSpace(settings.GetOnWithItMessage) // as typed input is
	if meta.IsDefined("get_on_with_it_message") && settings.GetOnWithItMessage == "" {
		return settings, fmt.Errorf("%s: get_on_with_it_message is empty", shownPath)
	}

	lists := []struct {
		name   string
		values *[]string
	}{
		{"skill.lookup", &settings.Skill.LookupDirs},
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
