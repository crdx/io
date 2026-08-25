package sessions

import (
	"testing"

	"crdx.org/io/agent"

	"crdx.org/io/cmd/oh/store"
)

func TestLoadingSessionsIdentifiesThoseThatAreRunning(t *testing.T) {
	directory := t.TempDir()
	writer, err := store.Create(directory, store.Meta{WorkspaceDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "begin"}); err != nil {
		t.Fatal(err)
	}

	loadedSessions, err := Load(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(loadedSessions) != 1 || !loadedSessions[0].IsRunning {
		t.Fatalf("expected one running session, got %+v", loadedSessions)
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	loadedSessions, err = Load(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(loadedSessions) != 1 || loadedSessions[0].IsRunning {
		t.Errorf("expected one stopped session, got %+v", loadedSessions)
	}
}
