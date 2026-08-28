package sessions

import (
	"strings"
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

func TestAWorkspaceWithNothingStoredSaysSo(t *testing.T) {
	directory := t.TempDir()
	workspaceDir := t.TempDir()

	writer, err := store.Create(directory, store.Meta{WorkspaceDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	var screen strings.Builder

	_, err = Choose(directory, workspaceDir, nil, &screen)
	if err == nil {
		t.Fatal("expected the empty workspace to be reported")
	}
	if err.Error() != "there are no stored conversations for this workspace" {
		t.Errorf("expected the workspace to be named as the empty one, got %v", err)
	}
	if screen.String() != "" {
		t.Errorf("expected nothing to be drawn, got %q", screen.String())
	}
}
