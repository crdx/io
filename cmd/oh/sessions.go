package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"

	"crdx.org/io/cmd/oh/picker"
	"crdx.org/io/session"
)

func chooseStoredSession(directory string, terminal *os.File, screen io.Writer) (string, error) {
	sessions, err := loadSessions(directory)
	if err != nil {
		return "", err
	}
	if len(sessions) == 0 {
		return "", errors.New("there are no stored conversations")
	}

	chosenSession, err := picker.Choose(sessions, terminal, screen)
	if errors.Is(err, picker.ErrCancelled) {
		return "", nil
	}
	if err != nil {
		return "", err
	}

	return chosenSession.Name, nil
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
