package sessions

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"crdx.org/io/cmd/oh/model"
	"crdx.org/io/cmd/oh/picker"
	"crdx.org/io/cmd/oh/sessionPicker"
	"crdx.org/io/cmd/oh/store"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/session"
)

func Choose(directory string, workspaceDir string, terminal *os.File, screen io.Writer) (string, error) {
	workspaceDir, err := ResolveWorkspaceDir(workspaceDir)
	if err != nil {
		return "", err
	}

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
	sessions = InWorkspace(sessions, workspaceDir)
	if len(sessions) == 0 {
		return "", errors.New("there are no stored conversations for this workspace")
	}

	chosenSession, err := sessionPicker.Choose(sessions, terminal, screen)
	if errors.Is(err, picker.ErrCancelled) {
		return "", nil
	}
	if err != nil {
		return "", err
	}

	return chosenSession.Name, nil
}

func ResolveWorkspaceDir(workspaceDir string) (string, error) {
	if workspaceDir == "" {
		workspaceDir = "."
	}

	workspaceDir, err := filepath.Abs(workspaceDir)
	if err != nil {
		return "", fmt.Errorf("could not resolve the workspace path: %w", err)
	}

	return workspaceDir, nil
}

func InWorkspace(sessions []*sessionPicker.Session, workspaceDir string) []*sessionPicker.Session {
	chosen := make([]*sessionPicker.Session, 0, len(sessions))
	for _, storedSession := range sessions {
		if filepath.Clean(storedSession.WorkspaceDir) == workspaceDir {
			chosen = append(chosen, storedSession)
		}
	}

	return chosen
}

// RefreshListings writes the listing metadata again for every session whose listing cannot be read,
// because a listing is an index over the journal rather than a record of its own, and a build that
// keeps it in a newer shape is entitled to rebuild what it finds.
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

	_, _ = fmt.Fprintln(screen, style.Subtle("writing the session listing again from the journals"))

	_, err = store.RebuildStaleMeta(directory)

	return err
}

func Load(directory string) ([]*sessionPicker.Session, error) {
	metadata, err := session.ListMeta(directory)
	if err != nil {
		return nil, err
	}

	sessions := make([]*sessionPicker.Session, 0, len(metadata))
	for _, storedMeta := range metadata {
		var data struct {
			WorkspaceDir string `json:"workspaceDir"`
			Model        string `json:"model"`
			Effort       string `json:"effort"`
		}
		if len(storedMeta.Data) > 0 && json.Unmarshal(storedMeta.Data, &data) != nil {
			continue
		}

		isRunning, err := session.IsInUse(directory, storedMeta.Name)
		if err != nil {
			return nil, err
		}

		sessions = append(sessions, &sessionPicker.Session{
			Name:         storedMeta.Name,
			WorkspaceDir: data.WorkspaceDir,
			Started:      storedMeta.Started,
			Touched:      storedMeta.Touched,
			Title:        storedMeta.Title,
			Model:        strings.Join(model.DisplayName(data.Model), " "),
			ModelID:      data.Model,
			Effort:       data.Effort,
			MessageCount: storedMeta.Messages,
			IsRunning:    isRunning,
		})
	}

	return sessions, nil
}
