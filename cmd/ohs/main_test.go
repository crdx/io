package main

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"crdx.org/io/agent"
	"crdx.org/io/session"
)

func TestSessionsComeFromOhsOwnJournalParsing(t *testing.T) {
	directory := t.TempDir()
	meta := json.RawMessage(`{"workspaceDir":"/home/alice/project","model":"gpt"}`)
	writer, err := session.Create(directory, meta)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Event(agent.Event{Kind: agent.Prompt, Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	sessions, err := loadSessions(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected one session, got %d", len(sessions))
	}
	if sessions[0].Name != writer.Name() || sessions[0].WorkspaceDir != "/home/alice/project" {
		t.Errorf("unexpected session: %+v", sessions[0])
	}
	if sessions[0].FirstPrompt() != "hello" {
		t.Errorf("expected the first prompt, got %q", sessions[0].FirstPrompt())
	}
}

func TestSessionsUseOhsStateDirectory(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", root)

	want := filepath.Join(root, "org.crdx", "oh", "sessions")
	if got := sessionsDir(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
