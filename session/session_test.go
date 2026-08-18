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

	head, err := os.ReadFile(filepath.Join(directory, writer.ID(), "session.jsonl")) //nolint:gosec // the test's own path
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

func TestJournalCarriesMetaEventsAndItems(t *testing.T) {
	directory := t.TempDir()
	meta := json.RawMessage(`{"world":"weather"}`)
	writer, err := session.Create(directory, meta)
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
	if string(storedSession.Meta) != string(meta) {
		t.Errorf("got meta %s, want %s", storedSession.Meta, meta)
	}
	if len(storedSession.Events) != 1 || storedSession.Events[0].Text != "will it rain?" {
		t.Errorf("got events %v", storedSession.Events)
	}
	if len(storedSession.Items) != 1 || string(storedSession.Items[0]) != `{"type":"message"}` {
		t.Errorf("got items %v", storedSession.Items)
	}
}
