package editor

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

func Open(configured string, path string) error {
	command, err := buildCommand(configured, path)
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

func buildCommand(configured string, path string) (*exec.Cmd, error) {
	name := strings.TrimSpace(configured)
	if name == "" {
		return nil, errors.New("editor is not configured: set editor in config.toml")
	}

	return exec.Command(name, path), nil //nolint:gosec // the user deliberately configures the editor
}

func reportExit(command *exec.Cmd, errors io.Writer) {
	if err := command.Wait(); err != nil {
		_, _ = fmt.Fprintf(errors, "editor exited: %v\n", err)
	}
}
