package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"crdx.org/duckopt/v2"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/ohs/picker"
	"crdx.org/io/internal/xdg"
	"crdx.org/io/session"
)

const usage = `ohs — oh session manager

Usage:
    $0

Options:
    -h, --help    Show this help
`

func main() {
	duckopt.MustBind[struct{}](usage, "$0")
	style.Init(os.Stdout)

	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	sessions, err := loadSessions(sessionsDir())
	if err != nil {
		return err
	}
	if len(sessions) == 0 {
		return errors.New("there are no stored conversations")
	}

	chosenSession, err := picker.Choose(sessions, os.Stdin, os.Stdout)
	if errors.Is(err, picker.ErrCancelled) {
		return nil
	}
	if err != nil {
		return err
	}

	return resume(chosenSession.Name)
}

func sessionsDir() string {
	return xdg.StatePath("org.crdx", "oh", "sessions")
}

func loadSessions(directory string) ([]*picker.Session, error) {
	storedSessions, err := session.List(directory)
	if err != nil {
		return nil, err
	}

	sessions := make([]*picker.Session, 0, len(storedSessions))
	for _, storedSession := range storedSessions {
		var meta struct {
			WorkspaceDir string `json:"workspaceDir"`
		}
		if len(storedSession.Meta) > 0 && json.Unmarshal(storedSession.Meta, &meta) != nil {
			continue
		}

		sessions = append(sessions, &picker.Session{
			Name:         storedSession.Name,
			WorkspaceDir: meta.WorkspaceDir,
			Touched:      storedSession.Touched,
			Events:       storedSession.Events,
		})
	}

	return sessions, nil
}

func resume(id string) error {
	oh, err := exec.LookPath("oh")
	if err != nil {
		return fmt.Errorf("could not find oh: %w", err)
	}

	//nolint:gosec // executing oh with the selected session is the command's purpose
	return syscall.Exec(oh, []string{oh, "-r", id}, os.Environ())
}
