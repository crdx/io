package editor

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
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

func buildCommand(configured Command, paths []string) (*exec.Cmd, error) {
	if len(configured) == 0 {
		return nil, errors.New("editor is not configured: set editor in config.toml")
	}
	name := strings.TrimSpace(configured[0])
	if name == "" {
		return nil, errors.New("editor is not configured: set editor in config.toml")
	}

	arguments := append([]string(nil), configured[1:]...)
	arguments = append(arguments, paths...)
	return exec.Command(name, arguments...), nil //nolint:gosec // the user deliberately configures the editor
}

func reportExit(command *exec.Cmd, errors io.Writer) {
	if err := command.Wait(); err != nil {
		_, _ = fmt.Fprintf(errors, "Editor exited: %v\n", err)
	}
}
