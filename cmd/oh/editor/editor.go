package editor

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

type Command []string

func (self *Command) UnmarshalTOML(value any) error {
	switch configured := value.(type) {
	case string:
		*self = Command{strings.TrimSpace(configured)}
		return nil
	case []any:
		command := make(Command, len(configured))
		for i, argument := range configured {
			text, ok := argument.(string)
			if !ok {
				return fmt.Errorf("editor argument %d is not a string", i+1)
			}
			command[i] = text
		}
		if len(command) > 0 {
			command[0] = strings.TrimSpace(command[0])
		}
		*self = command
		return nil
	default:
		return errors.New("editor is not a string or array of strings")
	}
}

func Open(configured Command, paths ...string) error {
	command, err := buildCommand(configured, paths)
	if err != nil {
		return err
	}
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		return fmt.Errorf("could not start editor: %w", err)
	}

	go reportExit(command, os.Stderr)

	return nil
}

var candidates = []Command{
	{"subl", "--wait"},
	{"code", "--wait"},
}

var terminalEditors = []string{
	"ed",
	"emacs",
	"helix",
	"hx",
	"jed",
	"joe",
	"kak",
	"mcedit",
	"micro",
	"nano",
	"ne",
	"nvim",
	"pico",
	"vi",
	"view",
	"vim",
}

func Detect() (Command, bool) {
	for _, candidate := range candidates {
		if _, err := exec.LookPath(candidate[0]); err == nil {
			return candidate, true
		}
	}

	return nil, false
}

func IsTerminalEditor(name string) bool {
	return slices.Contains(terminalEditors, filepath.Base(name))
}

func buildCommand(configured Command, paths []string) (*exec.Cmd, error) {
	if len(configured) == 0 || strings.TrimSpace(configured[0]) == "" {
		detected, found := Detect()
		if !found {
			return nil, errors.New("no editor was found: set editor in config.toml")
		}
		configured = detected
	}

	name := strings.TrimSpace(configured[0])
	if IsTerminalEditor(name) {
		return nil, fmt.Errorf(
			"%s is not supported yet: set a graphical editor in config.toml",
			name,
		)
	}

	arguments := append([]string(nil), configured[1:]...)
	arguments = append(arguments, paths...)
	//nolint:gosec,noctx // the user configures the editor, and it outlives this call
	return exec.Command(name, arguments...), nil
}

func reportExit(command *exec.Cmd, errors io.Writer) {
	if err := command.Wait(); err != nil {
		_, _ = fmt.Fprintf(errors, "Editor exited: %v\n", err)
	}
}
