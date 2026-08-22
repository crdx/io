package main

import (
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/internal/pathutil"
)

//go:embed defaults.toml
var defaultsTOML string

type Config struct {
	Provider           string `toml:"provider"`
	Model              string `toml:"model"`
	Effort             string `toml:"effort"`
	GetOnWithItMessage string `toml:"get_on_with_it_message"`

	Skill struct {
		Include []string `toml:"include"`
		Exclude []string `toml:"exclude"`
	} `toml:"skill"`
	Sandbox pathsConfig `toml:"sandbox"`
	Bar     barConfig   `toml:"bar"`

	fallback *toml.MetaData
	user     *toml.MetaData
	filePath string
}

type pathsConfig struct {
	Read  []string `toml:"read"`
	Write []string `toml:"write"`
	Exec  []string `toml:"exec"`
	Home  []string `toml:"home"`
}

type barConfig struct {
	Top    ruleConfig `toml:"top"`
	Bottom ruleConfig `toml:"bottom"`
}

type ruleConfig struct {
	Left   []toml.Primitive `toml:"left"`
	Center []toml.Primitive `toml:"center"`
	Right  []toml.Primitive `toml:"right"`
}

func (self barConfig) entries() map[segment.Position][]toml.Primitive {
	return map[segment.Position][]toml.Primitive{
		segment.TopLeft:      self.Top.Left,
		segment.TopCenter:    self.Top.Center,
		segment.TopRight:     self.Top.Right,
		segment.BottomLeft:   self.Bottom.Left,
		segment.BottomCenter: self.Bottom.Center,
		segment.BottomRight:  self.Bottom.Right,
	}
}

func (self Config) layout(set segment.Set) (segment.Layout, error) {
	layout := segment.Layout{}

	for position, entries := range self.Bar.entries() {
		meta := self.metaFor(position)

		for _, entry := range entries {
			var named struct {
				Segment string `toml:"segment"`
			}

			if err := meta.PrimitiveDecode(entry, &named); err != nil {
				return nil, fmt.Errorf("%s: %w", position, err)
			}

			read := func(into any) error { return meta.PrimitiveDecode(entry, into) }

			built, err := set.Build(named.Segment, position, read)
			if err != nil {
				return nil, err
			}

			layout[position] = append(layout[position], built)
		}
	}

	return layout, nil
}

func (self Config) metaFor(position segment.Position) *toml.MetaData {
	side, end, _ := strings.Cut(position.String(), ".")

	if self.user != nil && self.user.IsDefined("bar", side, end) {
		return self.user
	}

	return self.fallback
}

func (self Config) unknownKeys() error {
	if self.user == nil {
		return nil
	}

	unknown := self.user.Undecoded()
	if len(unknown) == 0 {
		return nil
	}

	named := make([]string, 0, len(unknown))
	for _, key := range unknown {
		named = append(named, key.String())
	}

	slices.Sort(named)

	return fmt.Errorf("%s: nothing is done with: %s", self.filePath, strings.Join(named, ", "))
}

func loadConfig(path string) (Config, error) {
	var config Config

	defaults, err := toml.Decode(defaultsTOML, &config)
	if err != nil {
		return config, fmt.Errorf("the built-in defaults are broken: %w", err)
	}

	config.fallback = &defaults

	if path == "" {
		return config, nil
	}

	displayPath := pathutil.Shorten(path)
	config.filePath = displayPath

	meta, err := toml.DecodeFile(path, &config)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return config, nil
	case err != nil:
		return config, fmt.Errorf("%s: %w", displayPath, err)
	}

	config.user = &meta

	if meta.IsDefined("provider") && !slices.Contains(providerNames, config.Provider) {
		return config, fmt.Errorf(
			"%s: provider must be one of: %s", displayPath, strings.Join(providerNames, ", "),
		)
	}
	if meta.IsDefined("model") && config.Model == "" {
		return config, fmt.Errorf("%s: model is empty, so there is nothing to ask", displayPath)
	}
	if meta.IsDefined("effort") && config.Effort == "" {
		return config, fmt.Errorf("%s: effort is empty, so there is nothing to ask for", displayPath)
	}
	config.GetOnWithItMessage = strings.TrimSpace(config.GetOnWithItMessage)
	if meta.IsDefined("get_on_with_it_message") && config.GetOnWithItMessage == "" {
		return config, fmt.Errorf("%s: get_on_with_it_message is empty", displayPath)
	}

	lists := []struct {
		name   string
		values *[]string
	}{
		{"skill.include", &config.Skill.Include},
		{"skill.exclude", &config.Skill.Exclude},
		{"sandbox.read", &config.Sandbox.Read},
		{"sandbox.write", &config.Sandbox.Write},
		{"sandbox.exec", &config.Sandbox.Exec},
		{"sandbox.home", &config.Sandbox.Home},
	}
	for _, list := range lists {
		for i, written := range *list.values {
			resolved, err := resolveConfigPath(path, written)
			if err != nil {
				return config, fmt.Errorf("%s: %s: %w", displayPath, list.name, err)
			}
			(*list.values)[i] = resolved
		}
	}

	for _, mapped := range config.Sandbox.Home {
		if _, below := homeRelativePath(mapped); !below {
			return config, fmt.Errorf(
				"%s: sandbox.home: %s is not below the home directory, so it has nowhere to land",
				displayPath, mapped,
			)
		}
	}

	return config, nil
}

func resolveConfigPath(configPath, written string) (string, error) {
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
		path = filepath.Join(filepath.Dir(configPath), path)
	}

	return filepath.Clean(path), nil
}
