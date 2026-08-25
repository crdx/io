package sessions

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"crdx.org/io/cmd/oh/picker"
	"crdx.org/io/cmd/oh/store"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/session"
)

func Choose(directory string, terminal *os.File, screen io.Writer) (string, error) {
	sessions, err := Load(directory)
	if errors.Is(err, session.ErrMetaOutOfDate) {
		_, _ = fmt.Fprintln(screen, style.Subtle("writing the session listing again from the journals"))
		if _, err := store.RebuildStaleMeta(directory); err != nil {
			return "", err
		}
		sessions, err = Load(directory)
	}
	if err != nil {
		if migrationError := ValidateFormats(directory); migrationError != nil {
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

func Load(directory string) ([]*picker.Session, error) {
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

		isRunning, err := session.IsInUse(directory, storedMeta.Name)
		if err != nil {
			return nil, err
		}

		sessions = append(sessions, &picker.Session{
			Name:         storedMeta.Name,
			WorkspaceDir: data.WorkspaceDir,
			Touched:      storedMeta.Touched,
			Title:        storedMeta.Title,
			MessageCount: storedMeta.Messages,
			IsRunning:    isRunning,
		})
	}

	return sessions, nil
}
