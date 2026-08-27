package config

import (
	_ "embed"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	"crdx.org/io/cmd/oh/editor"
	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/shell"
	"crdx.org/io/cmd/oh/snippets"

	"crdx.org/io/internal/format"
	"crdx.org/io/internal/util/pathutil"
)

//go:embed defaults.toml
var defaultsTOML string

type Config struct {
	Version            int                            `toml:"version"`
	Editor             Editor                         `toml:"editor"`
	Model              Model                          `toml:"model"`
	GetOnWithItMessage string                         `toml:"get_on_with_it_message"`
	Snippets           map[string]snippets.Definition `toml:"snippets"`
	Skills             SkillPaths                     `toml:"skills"`
	Sandbox            sandbox                        `toml:"sandbox"`
	Bar                Bar                            `toml:"bar"`

	fallback *toml.MetaData
	user     *toml.MetaData
	filePath string
}

type Editor struct {
	Command editor.Command `toml:"command"`
}

type Model struct {
	RoundRobin []string `toml:"round_robin"`
}

type SkillPaths struct {
	Include []string `toml:"include"`
	Exclude []string `toml:"exclude"`
}

type sandbox = shell.Paths

type Bar struct {
	Top    Rule `toml:"top"`
	Bottom Rule `toml:"bottom"`
}

type Rule struct {
	Left   []toml.Primitive `toml:"left"`
	Center []toml.Primitive `toml:"center"`
	Right  []toml.Primitive `toml:"right"`
}

type LiveConfig struct {
	GetOnWithItMessage string
	SegmentLayout      segment.Layout
}

func (self Config) BuildLive(registry segment.Registry) (LiveConfig, error) {
	layout, err := self.BuildLayout(registry)
	if err != nil {
		return LiveConfig{}, err
	}
	if err := self.ValidateConsumed(); err != nil {
		return LiveConfig{}, err
	}

	return LiveConfig{
		GetOnWithItMessage: self.GetOnWithItMessage,
		SegmentLayout:      layout,
	}, nil
}

func (self Bar) entries() map[segment.Position][]toml.Primitive {
	return map[segment.Position][]toml.Primitive{
		segment.TopLeft:      self.Top.Left,
		segment.TopCenter:    self.Top.Center,
		segment.TopRight:     self.Top.Right,
		segment.BottomLeft:   self.Bottom.Left,
		segment.BottomCenter: self.Bottom.Center,
		segment.BottomRight:  self.Bottom.Right,
	}
}

func (self Config) BuildLayout(registry segment.Registry) (segment.Layout, error) {
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

			options := segmentOptions{meta: meta, entry: entry}

			built, err := registry.Build(named.Segment, position, options)
			if err != nil {
				return nil, err
			}

			layout[position] = append(layout[position], built)
		}
	}

	return layout, nil
}

type segmentOptions struct {
	meta  *toml.MetaData
	entry toml.Primitive
}

func (self segmentOptions) Read(into any) error {
	return self.meta.PrimitiveDecode(self.entry, into)
}

func (self Config) ValidateConsumed() error {
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

func (self Config) metaFor(position segment.Position) *toml.MetaData {
	side, end, _ := strings.Cut(position.String(), ".")

	if self.user != nil && self.user.IsDefined("bar", side, end) {
		return self.user
	}

	return self.fallback
}

func readConfigVersion(data []byte) (int, error) {
	version, err := format.ReadTOML(data)
	if err != nil {
		return 0, err
	}
	if version == 0 {
		version = InitialFormat
	}

	return version, nil
}

func Load(path string) (Config, error) {
	if path == "" {
		return loadSnapshot(path, snapshot{isMissing: true})
	}
	return loadSnapshot(path, readSnapshot(path))
}

func loadSnapshot(path string, current snapshot) (Config, error) {
	var config Config

	defaults, err := toml.Decode(defaultsTOML, &config)
	if err != nil {
		return config, fmt.Errorf("the built-in defaults are broken: %w", err)
	}

	config.fallback = &defaults

	if path == "" || current.isMissing {
		return config, nil
	}

	displayPath := pathutil.Shorten(path)
	config.filePath = displayPath

	if current.failure != nil {
		return config, fmt.Errorf("%s: %w", displayPath, current.failure)
	}

	version, err := readConfigVersion(current.data)
	switch {
	case err != nil:
		return config, fmt.Errorf("%s: %w", displayPath, err)
	case version < Format:
		return config, fmt.Errorf("%s: config format %d needs migrating: run ohctl migrate", displayPath, version)
	}

	if err := format.Check(version, Format); err != nil {
		return config, fmt.Errorf("%s: config %w: upgrade oh", displayPath, err)
	}

	meta, err := toml.Decode(string(current.data), &config)
	if err != nil {
		return config, fmt.Errorf("%s: %w", displayPath, err)
	}

	config.user = &meta

	if meta.IsDefined("model", "round_robin") {
		if len(config.Model.RoundRobin) == 0 {
			return config, fmt.Errorf("%s: model.round_robin is empty, so there is nothing to ask", displayPath)
		}
		for _, selection := range config.Model.RoundRobin {
			if strings.TrimSpace(selection) == "" {
				return config, fmt.Errorf("%s: model.round_robin contains an empty selection", displayPath)
			}
		}
	}
	config.GetOnWithItMessage = strings.TrimSpace(config.GetOnWithItMessage)
	if meta.IsDefined("get_on_with_it_message") && config.GetOnWithItMessage == "" {
		return config, fmt.Errorf("%s: get_on_with_it_message is empty", displayPath)
	}
	for _, name := range slices.Sorted(maps.Keys(config.Snippets)) {
		definition := config.Snippets[name]
		if definition.File != "" {
			resolvedPath, err := resolveConfigPath(path, definition.File)
			if err != nil {
				return config, fmt.Errorf("%s: snippets.%s.file: %w", displayPath, name, err)
			}
			definition, err = definition.LoadFile(resolvedPath)
			if err != nil {
				return config, fmt.Errorf("%s: snippets.%s: %w", displayPath, name, err)
			}
		}
		config.Snippets[name] = definition
	}

	lists := []struct {
		name   string
		values *[]string
	}{
		{"skills.include", &config.Skills.Include},
		{"skills.exclude", &config.Skills.Exclude},
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
		if _, below := shell.HomeRelativePath(mapped); !below {
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
