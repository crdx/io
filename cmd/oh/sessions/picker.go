package sessions

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"crdx.org/io/cmd/oh/picker"
	"crdx.org/io/cmd/oh/store"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/session"
)

func Choose(directory string, workspaceDir string, terminal *os.File, screen io.Writer) (string, error) {
	workspaceDir, err := ResolveWorkspaceDir(workspaceDir)
	if err != nil {
		return "", err
	}

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
	sessions = InWorkspace(sessions, workspaceDir)
	if len(sessions) == 0 {
		return "", errors.New("there are no stored conversations for this workspace")
	}

	chosenSession, err := picker.Choose(sessions, workspaceDir, terminal, screen)
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

func InWorkspace(sessions []*picker.Session, workspaceDir string) []*picker.Session {
	chosen := make([]*picker.Session, 0, len(sessions))
	for _, storedSession := range sessions {
		if filepath.Clean(storedSession.WorkspaceDir) == workspaceDir {
			chosen = append(chosen, storedSession)
		}
	}

	return chosen
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
			Started:      storedMeta.Started,
			Touched:      storedMeta.Touched,
			Title:        storedMeta.Title,
			MessageCount: storedMeta.Messages,
			IsRunning:    isRunning,
		})
	}

	return sessions, nil
}
