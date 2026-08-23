package main

import (
	"encoding/json"
	"testing"

	"crdx.org/io/agent"
	"crdx.org/io/session"
)

func TestSessionsComeFromJournalParsing(t *testing.T) {
	directory := t.TempDir()
	meta := json.RawMessage(`{"workspaceDir":"/home/alice/project","model":"gpt"}`)
	writer, err := session.Create(directory, meta)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "hello"}); err != nil {
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
	if sessions[0].FirstMessage() != "hello" {
		t.Errorf("expected the first message, got %q", sessions[0].FirstMessage())
	}
}

func TestChoosingWithoutStoredSessionsFails(t *testing.T) {
	if _, err := chooseStoredSession(t.TempDir(), nil, nil); err == nil {
		t.Error("expected an empty session list to fail")
	}
}
