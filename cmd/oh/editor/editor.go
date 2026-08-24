package editor

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func Open(configured string, path string) error {
	command, err := buildCommand(configured, path)
	if err != nil {
		return err
	}
	if err := command.Start(); err != nil {
		return fmt.Errorf("could not start editor: %w", err)
	}

	go func() { _ = command.Wait() }()

	return nil
}

func buildCommand(configured string, path string) (*exec.Cmd, error) {
	name := strings.TrimSpace(configured)
	if name == "" {
		name = strings.TrimSpace(os.Getenv("VISUAL"))
	}
	if name == "" {
		return nil, errors.New("no editor is configured and VISUAL is not set")
	}

	return exec.Command(name, path), nil //nolint:gosec // the user deliberately configures the editor
}
