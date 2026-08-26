package snippets

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
)

type ArgumentPolicy string

const (
	ArgumentsRequired ArgumentPolicy = "required"
	ArgumentsOptional ArgumentPolicy = "optional"
	ArgumentsNone     ArgumentPolicy = "none"
)

type Definition struct {
	Prompt      string
	File        string
	Description string
	Arguments   ArgumentPolicy
}

func (self *Definition) UnmarshalTOML(value any) error {
	switch configured := value.(type) {
	case string:
		self.Prompt = strings.TrimSpace(configured)
		if self.Prompt == "" {
			return errors.New("prompt is empty")
		}
		return nil
	case map[string]any:
		return self.unmarshalTable(configured)
	default:
		return errors.New("definition is not a prompt or a table")
	}
}

func (self Definition) LoadFile(path string) (Definition, error) {
	contents, err := os.ReadFile(path) //nolint:gosec // the user deliberately configures this path
	if err != nil {
		return self, fmt.Errorf("could not read %s: %w", path, err)
	}
	self.File = path
	self.Prompt = strings.TrimSpace(string(contents))
	if self.Prompt == "" {
		return self, errors.New("prompt file is empty")
	}
	return self, nil
}

func (self *Definition) unmarshalTable(configured map[string]any) error {
	known := map[string]bool{
		"arguments":   true,
		"description": true,
		"file":        true,
		"prompt":      true,
	}
	var unknown []string
	for name := range configured {
		if !known[name] {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		slices.Sort(unknown)
		return fmt.Errorf("nothing is done with: %s", strings.Join(unknown, ", "))
	}

	var err error
	if self.Prompt, err = getString(configured, "prompt"); err != nil {
		return err
	}
	if self.File, err = getString(configured, "file"); err != nil {
		return err
	}
	if self.Description, err = getString(configured, "description"); err != nil {
		return err
	}
	arguments, err := getString(configured, "arguments")
	if err != nil {
		return err
	}
	self.Arguments = ArgumentPolicy(arguments)

	if (self.Prompt == "") == (self.File == "") {
		return errors.New("set exactly one of prompt or file")
	}
	if strings.ContainsAny(self.Description, "\r\n") {
		return errors.New("description must fit on one line")
	}
	switch self.Arguments {
	case ArgumentsRequired, ArgumentsOptional, ArgumentsNone, "":
		return nil
	default:
		return fmt.Errorf("arguments is %q, want required, optional, or none", self.Arguments)
	}
}

func getString(configured map[string]any, name string) (string, error) {
	value, found := configured[name]
	if !found {
		return "", nil
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s is not a string", name)
	}
	return strings.TrimSpace(text), nil
}
