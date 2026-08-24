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
		if migrationError := refuseUnreadableSessions(directory); migrationError != nil {
			return "", migrationError
		}
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
	metadata, err := session.ListMeta(directory)
	if err != nil {
		return nil, err
	}

	sessions := make([]*picker.Session, 0, len(metadata))
	for _, storedMeta := range metadata {
		var data struct {
			WorkspaceDir string `json:"workspaceDir"`
		}
		if len(storedMeta.Data) > 0 && json.Unmarshal(storedMeta.Data, &data) != nil {
			continue
		}

		sessions = append(sessions, &picker.Session{
			Name:         storedMeta.Name,
			WorkspaceDir: data.WorkspaceDir,
			Touched:      storedMeta.Touched,
			Title:        storedMeta.Title,
			MessageCount: storedMeta.Messages,
		})
	}

	return sessions, nil
}
