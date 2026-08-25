package sessions

import (
	"errors"
	"fmt"
	"path/filepath"

	"crdx.org/io/session"

	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/store"
)

const (
	passedSessionPrompt   = "Read chat.md first, then continue." //nolint:gosec // an opening prompt, not a credential
	sessionTranscriptName = "chat.md"
)

type PassedSession struct {
	WorkspaceDir       string
	InitialFilePath    string
	InitialUserMessage string
}

func GetPassedSession(directory string, name string, userMessage string) (*PassedSession, error) {
	if name == "" {
		return nil, nil
	}

	storedSession, err := store.Read(directory, name)
	if err != nil {
		return nil, err
	}

	initialUserMessage := passedSessionPrompt
	if userMessage != "" {
		initialUserMessage += "\n\n" + userMessage
	}

	return &PassedSession{
		WorkspaceDir:       storedSession.Meta.WorkspaceDir,
		InitialFilePath:    filepath.Join(directory, storedSession.Name, sessionTranscriptName),
		InitialUserMessage: initialUserMessage,
	}, nil
}

func LoadForResume(directory string, name string) (*store.Session, error) {
	if name == "" {
		return nil, nil
	}

	isRunning, err := session.IsInUse(directory, name)
	if err != nil {
		return nil, err
	}
	if isRunning {
		return nil, fmt.Errorf("session %s is running", name)
	}

	storedSession, err := store.Read(directory, name)
	if err != nil {
		return nil, err
	}
	if !storedSession.CanResume() {
		return nil, fmt.Errorf("session %s did not finish every turn and cannot be resumed safely (yet)", name)
	}

	return storedSession, nil
}

func OpenWriter(directory string, resumedSession *store.Session, meta store.Meta) (*store.Writer, error) {
	if resumedSession == nil {
		return store.Create(directory, meta)
	}

	log, err := store.Open(directory, resumedSession.Name)
	if errors.Is(err, session.ErrInUse) {
		return nil, fmt.Errorf("session %s is running", resumedSession.Name)
	}

	return log, err
}

func OpeningCaps(requestedCaps caps.Set, wereCapsChosen bool, resumedSession *store.Session) (caps.Set, error) {
	if resumedSession == nil {
		return requestedCaps, nil
	}

	lastCaps, found := caps.LastRecordedMode(resumedSession.Events)
	if !found {
		return requestedCaps, nil
	}

	if wereCapsChosen && requestedCaps != lastCaps {
		return 0, fmt.Errorf(
			"a resumed conversation opens in the mode it was left in, which was %s rather than %s",
			lastCaps.Flags(),
			requestedCaps.Flags(),
		)
	}

	return lastCaps, nil
}

func ResumeCommand(programPath string, name string) string {
	return fmt.Sprintf("%s -r %s", filepath.Base(programPath), name)
}
