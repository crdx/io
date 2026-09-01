package sessions

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"crdx.org/io/cmd/oh/menu"
	"crdx.org/io/cmd/oh/model"
	"crdx.org/io/cmd/oh/sessions/picker"
	"crdx.org/io/cmd/oh/store"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/oh/work"
	"crdx.org/io/session"
)

func Choose(directory string, workspace *work.Space, terminal *os.File, screen io.Writer) (string, error) {
	if err := RefreshListings(directory, screen); err != nil {
		return "", err
	}

	sessions, err := Load(directory)
	if err != nil {
		if migrationError := ValidateFormats(directory); migrationError != nil {
			return "", migrationError
		}
		return "", err
	}
	sessions = InWorkspace(sessions, workspace)
	if len(sessions) == 0 {
		return "", errors.New("there are no stored conversations for this workspace")
	}

	chosenSession, err := picker.Choose(sessions, terminal, screen)
	if errors.Is(err, menu.ErrCancelled) {
		return "", nil
	}
	if err != nil {
		return "", err
	}

	return chosenSession.Name, nil
}

func InWorkspace(sessions []*picker.Session, workspace *work.Space) []*picker.Session {
	chosenSessions := make([]*picker.Session, 0, len(sessions))
	for _, storedSession := range sessions {
		if workspace.IsAt(storedSession.WorkspaceDir) {
			chosenSessions = append(chosenSessions, storedSession)
		}
	}

	return chosenSessions
}

func RefreshListings(directory string, screen io.Writer) error {
	stale, err := store.StaleMeta(directory)
	if err != nil {
		return err
	}
	if len(stale) == 0 {
		return nil
	}

	if formatError := ValidateFormats(directory); formatError != nil {
		return formatError
	}

	rebuilt := 0
	for _, name := range stale {
		wasRebuilt, err := store.RebuildMetaIfIdle(directory, name)
		if err != nil {
			return err
		}
		if wasRebuilt {
			rebuilt++
		}
	}
	if rebuilt > 0 {
		_, _ = fmt.Fprintln(screen, style.Subtle("writing the session listing again from the journals"))
	}
	return nil
}

func Load(directory string) ([]*picker.Session, error) {
	entries, err := session.Entries(directory)
	if err != nil {
		return nil, err
	}

	sessions := make([]*picker.Session, 0, len(entries))
	for _, entry := range entries {
		isRunning, err := session.IsInUse(directory, entry.Name)
		if err != nil {
			return nil, err
		}

		storedMeta, err := session.ReadMeta(directory, entry.Name)
		if err != nil && isRunning {
			storedMeta, err = store.GetListingMeta(directory, entry.Name)
		}
		if err != nil {
			return nil, fmt.Errorf("could not read session %s metadata: %w", entry.Name, err)
		}

		var data struct {
			WorkspaceDir string `json:"workspaceDir"`
			Model        string `json:"model"`
			Effort       string `json:"effort"`
		}
		if len(storedMeta.Data) > 0 && json.Unmarshal(storedMeta.Data, &data) != nil {
			continue
		}

		sessions = append(sessions, &picker.Session{
			Name:         storedMeta.Name,
			WorkspaceDir: data.WorkspaceDir,
			StartedAt:    storedMeta.StartedAt,
			TouchedAt:    storedMeta.TouchedAt,
			Title:        storedMeta.Title,
			Model:        strings.Join(model.DisplayName(data.Model), " "),
			ModelID:      data.Model,
			Effort:       data.Effort,
			MessageCount: storedMeta.Messages,
			IsRunning:    isRunning,
		})
	}

	slices.SortFunc(sessions, func(first, second *picker.Session) int {
		if order := second.TouchedAt.Compare(first.TouchedAt); order != 0 {
			return order
		}
		return strings.Compare(second.Name, first.Name)
	})
	return sessions, nil
}
