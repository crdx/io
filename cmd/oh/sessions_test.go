package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/store"
	"crdx.org/io/session"
)

func writeStoredSession(t *testing.T, directory string, name string, started string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Join(directory, name), 0o700); err != nil {
		t.Fatal(err)
	}

	head := fmt.Sprintf(`{"kind":"head","time":%q,"id":%q,"name":%q}`+"\n", started, name, name)
	if err := os.WriteFile(filepath.Join(directory, name, "session.jsonl"), []byte(head), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSessionsComeFromJournalParsing(t *testing.T) {
	directory := t.TempDir()
	meta := json.RawMessage(`{"workspaceDir":"/home/alice/project","model":"gpt"}`)
	writer, err := session.Create(directory, meta, meta)
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
	if sessions[0].Title != "hello" {
		t.Errorf("expected the provisional title, got %q", sessions[0].Title)
	}
}

func TestChoosingWithoutStoredSessionsFails(t *testing.T) {
	if _, err := chooseStoredSession(t.TempDir(), nil, nil); err == nil {
		t.Error("expected an empty session list to fail")
	}
}

func TestLoadingACrashedSessionIsRefusedWithoutChangingItsJournal(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	writer, err := store.Create(sessionsDir(), store.Meta{WorkspaceDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "begin"}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Event(agent.Event{Kind: agent.ModelMessageEvent, Text: "looks complete"}); err != nil {
		t.Fatal(err)
	}
	name := writer.Name()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(sessionsDir(), name, "session.jsonl")
	before, err := os.ReadFile(path) //nolint:gosec // the test's own session
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loadSession(name); err == nil || !strings.Contains(err.Error(), "did not finish every turn") {
		t.Fatalf("expected the crashed session to be refused, got %v", err)
	}
	match, err := os.ReadFile(path) //nolint:gosec // the test's own session
	if err != nil {
		t.Fatal(err)
	}
	if string(match) != string(before) {
		t.Error("refusing the crashed session changed its journal")
	}
}

func TestLoadingACompletedSessionSucceeds(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	writer, err := store.Create(sessionsDir(), store.Meta{WorkspaceDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "begin"}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Item(json.RawMessage(`{"role":"user"}`)); err != nil {
		t.Fatal(err)
	}
	if err := writer.CompleteTurn(); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := loadSession(writer.Name()); err != nil {
		t.Fatalf("expected the completed session to load: %v", err)
	}
}

func TestChoosingASessionFromANewerOhAdvisesAnUpgrade(t *testing.T) {
	directory := t.TempDir()
	name := "able-dolphin"

	if err := os.MkdirAll(filepath.Join(directory, name), 0o700); err != nil {
		t.Fatal(err)
	}

	head := fmt.Sprintf(
		`{"kind":"head","time":"2026-08-01T00:00:00Z","version":%d,"id":%q,"name":%q}`+"\n",
		session.Format+1, name, name,
	)
	if err := os.WriteFile(filepath.Join(directory, name, "session.jsonl"), []byte(head), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := chooseStoredSession(directory, nil, nil)
	if err == nil {
		t.Fatal("expected the newer session to be refused")
	}
	if !strings.Contains(err.Error(), "upgrade oh") {
		t.Errorf("expected an upgrade to be advised, got %v", err)
	}
	if strings.Contains(err.Error(), "ohctl migrate") {
		t.Errorf("expected migration not to be advised for a newer format, got %v", err)
	}
}

func TestChoosingAnOutdatedSessionAdvisesMigration(t *testing.T) {
	directory := t.TempDir()
	writeStoredSession(t, directory, "able-dolphin", "2026-08-01T00:00:00Z")

	_, err := chooseStoredSession(directory, nil, nil)
	if err == nil {
		t.Fatal("expected the outdated session to be refused")
	}
	if !strings.Contains(err.Error(), "run `ohctl migrate`") {
		t.Errorf("expected migration advice, got %v", err)
	}
	if strings.Contains(err.Error(), "meta.json") {
		t.Errorf("expected the missing metadata implementation detail to be hidden, got %v", err)
	}
}
