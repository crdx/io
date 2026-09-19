package config

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"crdx.org/io/cmd/oh/permission"
	"crdx.org/io/tool"
	"crdx.org/io/tool/command"
)

const toolsSetting = "tools"

type DeclaredTool struct {
	Description string              `toml:"description"`
	Command     []string            `toml:"command"`
	Parameters  []DeclaredParameter `toml:"parameters"`
	Subject     string              `toml:"subject"`
	Timeout     time.Duration       `toml:"timeout"`
	Permission  string              `toml:"permission"`
	Default     *bool               `toml:"default"`
}

type DeclaredParameter struct {
	Name        string   `toml:"name"`
	Kind        string   `toml:"kind"`
	Description string   `toml:"description"`
	Values      []string `toml:"values"`
	IsOptional  bool     `toml:"optional"`
}

func (self DeclaredTool) IsDefault() bool {
	return self.Default == nil || *self.Default
}

func (self DeclaredTool) IsReference() bool {
	return len(self.Command) == 0
}

func (self DeclaredTool) checkReference() error {
	for setting, isWritten := range map[string]bool{
		"description": strings.TrimSpace(self.Description) != "",
		"parameters":  len(self.Parameters) > 0,
		"subject":     strings.TrimSpace(self.Subject) != "",
		"timeout":     self.Timeout != 0,
		"permission":  strings.TrimSpace(self.Permission) != "",
	} {
		if isWritten {
			return fmt.Errorf(
				"%s names a tool this harness already offers, so it may say whether it is a default and nothing else",
				setting,
			)
		}
	}

	return nil
}

func (self Config) BuildDeclaredTools(options command.Options) ([]tool.Tool, error) {
	tools := make([]tool.Tool, 0, len(self.Tools))

	for _, name := range slices.Sorted(maps.Keys(self.Tools)) {
		if self.Tools[name].IsReference() {
			if err := self.Tools[name].checkReference(); err != nil {
				return nil, self.complain(name, err)
			}

			continue
		}

		declaredTool, err := self.buildDeclaredTool(name, options)
		if err != nil {
			return nil, self.complain(name, err)
		}

		tools = append(tools, declaredTool)
	}

	return tools, nil
}

func (self Config) complain(name string, err error) error {
	setting := toolsSetting + "." + name
	if path := self.getSourcePath(toolsSetting, name); path != "" {
		setting = path + ": " + setting
	}

	return fmt.Errorf("%s: %w", setting, err)
}

func (self Config) NonDefaultToolNames() []string {
	var names []string

	for _, name := range slices.Sorted(maps.Keys(self.Tools)) {
		if !self.Tools[name].IsDefault() {
			names = append(names, name)
		}
	}

	return names
}

func (self Config) ToolReferenceNames() []string {
	var names []string

	for _, name := range slices.Sorted(maps.Keys(self.Tools)) {
		if self.Tools[name].IsReference() {
			names = append(names, name)
		}
	}

	return names
}

func (self Config) buildDeclaredTool(name string, options command.Options) (tool.Tool, error) {
	declaration, err := self.declare(name)
	if err != nil {
		return nil, err
	}

	return command.New(declaration, options)
}

func (self Config) declare(name string) (command.Declaration, error) {
	declaration := self.Tools[name]

	rule := permission.Ask
	if strings.TrimSpace(declaration.Permission) != "" {
		var err error
		if rule, err = permission.ParseRule(declaration.Permission); err != nil {
			return command.Declaration{}, fmt.Errorf("permission: %w", err)
		}
	}

	resolvedCommand, err := self.resolveCommand(name, declaration.Command)
	if err != nil {
		return command.Declaration{}, err
	}

	parameters := make([]command.Parameter, 0, len(declaration.Parameters))

	for _, parameter := range declaration.Parameters {
		parameters = append(parameters, command.Parameter{
			Name:        parameter.Name,
			Kind:        command.Kind(parameter.Kind),
			Description: parameter.Description,
			Values:      parameter.Values,
			IsOptional:  parameter.IsOptional,
		})
	}

	return command.Declaration{
		Name:        name,
		Description: declaration.Description,
		Command:     resolvedCommand,
		Parameters:  parameters,
		Subject:     declaration.Subject,
		TimeLimit:   declaration.Timeout,
		MustAsk:     rule != permission.Allow,
	}, nil
}

type Toolbox struct {
	Tools  map[string]DeclaredTool `toml:"tools"`
	Prompt EnvironmentPrompt       `toml:"prompt"`
}

type EnvironmentPrompt struct {
	Text string
	File string
}

func (self *EnvironmentPrompt) UnmarshalTOML(value any) error {
	switch contents := value.(type) {
	case string:
		self.Text = strings.TrimSpace(contents)
		if self.Text == "" {
			return errors.New("prompt is empty")
		}

		return nil
	case map[string]any:
		path, isPath := contents["file"].(string)
		if !isPath || strings.TrimSpace(path) == "" {
			return errors.New("prompt is a table, so it wants a file")
		}
		self.File = path

		return nil
	default:
		return errors.New("prompt is not text or a table naming a file")
	}
}

func LoadToolbox(path string) (Toolbox, error) {
	current := readSnapshot(path)
	switch {
	case current.failure != nil:
		return Toolbox{}, current.failure
	case current.isMissing:
		return Toolbox{}, fs.ErrNotExist
	}

	var contents Toolbox

	meta, err := toml.Decode(string(current.data), &contents)
	if err != nil {
		return Toolbox{}, err
	}

	if unknown := meta.Undecoded(); len(unknown) > 0 {
		names := make([]string, 0, len(unknown))
		for _, key := range unknown {
			names = append(names, key.String())
		}
		slices.Sort(names)

		return Toolbox{}, fmt.Errorf("unknown: %s", strings.Join(names, ", "))
	}

	for _, name := range slices.Sorted(maps.Keys(contents.Tools)) {
		declaration := contents.Tools[name]

		resolvedCommand, err := resolveCommandAgainst(path, declaration.Command)
		if err != nil {
			return Toolbox{}, fmt.Errorf("%s.%s.command: %w", toolsSetting, name, err)
		}

		declaration.Command = resolvedCommand
		contents.Tools[name] = declaration
	}

	if contents.Prompt.File != "" {
		resolvedPath, err := resolveConfigPath(path, contents.Prompt.File)
		if err != nil {
			return Toolbox{}, fmt.Errorf("prompt.file: %w", err)
		}

		current := readSnapshot(resolvedPath)
		switch {
		case current.failure != nil:
			return Toolbox{}, fmt.Errorf("prompt.file: could not read %s: %w", resolvedPath, current.failure)
		case current.isMissing:
			return Toolbox{}, fmt.Errorf("prompt.file: %s: %w", resolvedPath, fs.ErrNotExist)
		}

		contents.Prompt.Text = strings.TrimSpace(string(current.data))
		if contents.Prompt.Text == "" {
			return Toolbox{}, fmt.Errorf("prompt.file: %s is empty", resolvedPath)
		}
	}

	return contents, nil
}

func OfferedNames(namedTools []string, declarations map[string]DeclaredTool) []string {
	names := slices.Clone(namedTools)

	for _, name := range slices.Sorted(maps.Keys(declarations)) {
		if declarations[name].IsDefault() && !slices.Contains(names, name) {
			names = append(names, name)
		}
	}

	return names
}

func (self Config) WithTools(declarations map[string]DeclaredTool) Config {
	tools := maps.Clone(self.Tools)
	if tools == nil {
		tools = make(map[string]DeclaredTool, len(declarations))
	}

	maps.Copy(tools, declarations)
	self.Tools = tools

	return self
}

func (self Config) resolveCommand(name string, writtenCommand []string) ([]string, error) {
	resolvedCommand, err := resolveCommandAgainst(self.getSourceFile(toolsSetting, name), writtenCommand)
	if err != nil {
		return nil, fmt.Errorf("command: %w", err)
	}

	return resolvedCommand, nil
}

func resolveCommandAgainst(sourcePath string, writtenCommand []string) ([]string, error) {
	resolvedCommand := slices.Clone(writtenCommand)

	for at, word := range resolvedCommand {
		if !isCommandPath(at, word) {
			continue
		}

		path, err := resolveConfigPath(sourcePath, word)
		if err != nil {
			return nil, err
		}
		resolvedCommand[at] = path
	}

	return resolvedCommand, nil
}

func isCommandPath(at int, word string) bool {
	if at == 0 {
		return strings.ContainsRune(word, '/')
	}

	return strings.HasPrefix(word, "./") || strings.HasPrefix(word, "../")
}
