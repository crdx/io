package session_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/io/agent"
	"crdx.org/io/session"
)

// A file carried out of the directory it was written in still says which session it is, rather than
// leaving its name the only record of that.
func TestTheHeadCarriesTheSessionID(t *testing.T) {
	directory := t.TempDir()

	writer, err := session.Create(directory, nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := writer.Event(agent.Event{Kind: agent.Prompt, Text: "hello"}); err != nil {
		t.Fatal(err)
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	head, err := os.ReadFile(filepath.Join(directory, writer.ID()+".jsonl")) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}

	var line struct {
		Kind string `json:"kind"`
		ID   string `json:"id"`
	}

	if err := json.Unmarshal([]byte(strings.SplitN(string(head), "\n", 2)[0]), &line); err != nil {
		t.Fatal(err)
	}

	if line.Kind != "head" {
		t.Fatalf("expected the first line to be the head, got %q", line.Kind)
	}

	if line.ID != writer.ID() {
		t.Errorf("got the ID %q in the head, want %q", line.ID, writer.ID())
	}
}

// A session written before the head carried an ID is still read by the name it is stored under.
func TestASessionWithNoIDInItsHeadIsStillRead(t *testing.T) {
	directory := t.TempDir()
	legacyJournal := `{"kind":"head","time":"2020-01-01T00:00:00Z","metadata":{"model":"gpt"}}` + "\n"

	if err := os.WriteFile(filepath.Join(directory, "older.jsonl"), []byte(legacyJournal), 0o600); err != nil {
		t.Fatal(err)
	}

	read, err := session.Read(directory, "older")
	if err != nil {
		t.Fatal(err)
	}

	if read.ID != "older" {
		t.Errorf("got the ID %q, want %q", read.ID, "older")
	}
}

func TestJournalCarriesMetadataEventsAndItems(t *testing.T) {
	directory := t.TempDir()
	metadata := json.RawMessage(`{"world":"weather"}`)
	writer, err := session.Create(directory, metadata)
	if err != nil {
		t.Fatal(err)
	}

	if err := writer.Event(agent.Event{Kind: agent.Prompt, Text: "will it rain?"}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Item(json.RawMessage(`{"type":"message"}`)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	storedSession, err := session.Read(directory, writer.ID())
	if err != nil {
		t.Fatal(err)
	}
	if string(storedSession.Metadata) != string(metadata) {
		t.Errorf("got metadata %s, want %s", storedSession.Metadata, metadata)
	}
	if len(storedSession.Events) != 1 || storedSession.Events[0].Text != "will it rain?" {
		t.Errorf("got events %v", storedSession.Events)
	}
	if len(storedSession.Items) != 1 || string(storedSession.Items[0]) != `{"type":"message"}` {
		t.Errorf("got items %v", storedSession.Items)
	}
}
