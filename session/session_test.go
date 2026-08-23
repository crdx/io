package session_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/io/agent"
	"crdx.org/io/session"
)

func TestTheHeadCarriesTheNameAndTheIdentifier(t *testing.T) {
	directory := t.TempDir()

	writer, err := session.Create(directory, nil)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "hello"}); err != nil {
		t.Fatal(err)
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	head, err := os.ReadFile(filepath.Join(directory, writer.Name(), "session.jsonl")) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}

	var line struct {
		Kind string `json:"kind"`
		Name string `json:"name"`
		ID   string `json:"id"`
	}

	if err := json.Unmarshal([]byte(strings.SplitN(string(head), "\n", 2)[0]), &line); err != nil {
		t.Fatal(err)
	}

	if line.Kind != "head" {
		t.Fatalf("expected the first line to be the head, got %q", line.Kind)
	}

	if line.Name != writer.Name() {
		t.Errorf("got the name %q in the head, want %q", line.Name, writer.Name())
	}

	if line.ID != writer.ID() {
		t.Errorf("got the identifier %q in the head, want %q", line.ID, writer.ID())
	}
}

func TestJournalCarriesMetaEventsAndItems(t *testing.T) {
	directory := t.TempDir()
	meta := json.RawMessage(`{"world":"weather"}`)
	writer, err := session.Create(directory, meta)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "will it rain?"}); err != nil {
		t.Fatal(err)
	}
	state := json.RawMessage(`{"path":"weather.txt","sha256":"abc"}`)
	if _, err := writer.Event(agent.Event{Kind: agent.StateChangeEvent, Name: "file_read", State: state}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Item(json.RawMessage(`{"type":"message"}`)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	storedSession, err := session.Read(directory, writer.Name())
	if err != nil {
		t.Fatal(err)
	}
	if string(storedSession.Meta) != string(meta) {
		t.Errorf("got meta %s, want %s", storedSession.Meta, meta)
	}
	if len(storedSession.Events) != 2 || storedSession.Events[0].Text != "will it rain?" {
		t.Errorf("got events %v", storedSession.Events)
	}
	if storedState := storedSession.Events[1]; storedState.Kind != agent.StateChangeEvent || string(storedState.State) != string(state) {
		t.Errorf("got stored state %#v", storedState)
	}
	if len(storedSession.Items) != 1 || string(storedSession.Items[0]) != `{"type":"message"}` {
		t.Errorf("got items %v", storedSession.Items)
	}
}

func storedSession(t *testing.T, directory string) *session.Writer {
	t.Helper()

	writer, err := session.Create(directory, nil)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "hello"}); err != nil {
		t.Fatal(err)
	}

	return writer
}

func TestASessionInUseIsRefusedToASecondWriter(t *testing.T) {
	directory := t.TempDir()
	writer := storedSession(t, directory)
	defer func() { _ = writer.Close() }()

	second, err := session.Open(directory, writer.Name())
	if !errors.Is(err, session.ErrInUse) {
		_ = second.Close()
		t.Fatalf("expected the second writer to be refused, got %v", err)
	}
}

func TestASessionInUseIsGivenUpOnceItIsClosed(t *testing.T) {
	directory := t.TempDir()
	writer := storedSession(t, directory)

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := session.Open(directory, writer.Name())
	if err != nil {
		t.Fatalf("expected the closed session to be free, got %v", err)
	}

	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestAJournalSaysWhichFormatItWasWrittenIn(t *testing.T) {
	directory := t.TempDir()

	writer, err := session.Create(directory, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.EnsureStored(); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	entries, err := session.Entries(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one stored session, got %d", len(entries))
	}
	if entries[0].Format != session.Format {
		t.Errorf("expected format %d, got %d", session.Format, entries[0].Format)
	}

	outdated, err := session.Outdated(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(outdated) != 0 {
		t.Errorf("expected a session just written to be current, got %v", outdated)
	}
}

func TestAJournalWithoutAVersionCountsAsTheFirstFormat(t *testing.T) {
	directory := t.TempDir()
	name := "brave-otter"

	if err := os.MkdirAll(filepath.Join(directory, name), 0o750); err != nil {
		t.Fatal(err)
	}

	head := `{"kind":"head","time":"2026-08-01T00:00:00Z","id":"one","name":"brave-otter"}` + "\n"
	if err := os.WriteFile(filepath.Join(directory, name, "session.jsonl"), []byte(head), 0o600); err != nil {
		t.Fatal(err)
	}

	entries, err := session.Entries(directory)
	if err != nil {
		t.Fatal(err)
	}
	if entries[0].Format != 1 {
		t.Errorf("expected an unnumbered journal to count as the first format, got %d", entries[0].Format)
	}

	outdated, err := session.Outdated(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(outdated) != 1 || outdated[0] != name {
		t.Errorf("expected the old journal to be named as outdated, got %v", outdated)
	}
}
