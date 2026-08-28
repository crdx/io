package sessions

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/io/agent"
	"crdx.org/io/session"

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

func TestAListingInAnOlderFormatIsWrittenAgainRatherThanRefused(t *testing.T) {
	directory := t.TempDir()
	writer, err := store.Create(directory, store.Meta{WorkspaceDir: t.TempDir(), Model: "gpt-5.6-sol"})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "begin"}); err != nil {
		t.Fatal(err)
	}
	name := writer.Name()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	putListingBehind(t, directory, name)

	if _, err := session.Open(directory, name); !errors.Is(err, session.ErrMetaOutOfDate) {
		t.Fatalf("expected an older listing to stop the session being opened, got %v", err)
	}

	var said strings.Builder
	if err := RefreshListings(directory, &said); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(said.String(), "writing the session listing again") {
		t.Errorf("expected the rebuild to be reported, got %q", said.String())
	}

	reopened, err := session.Open(directory, name)
	if err != nil {
		t.Fatalf("expected the session to open once its listing was written again, got %v", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}

	loadedSessions, err := Load(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(loadedSessions) != 1 || loadedSessions[0].ModelID != "gpt-5.6-sol" {
		t.Errorf("expected the model to come back from the journal, got %+v", loadedSessions)
	}
	if loadedSessions[0].Model != "GPT Sol 5.6" {
		t.Errorf("expected the model to be named for a person, got %q", loadedSessions[0].Model)
	}
}

func putListingBehind(t *testing.T, directory string, name string) {
	t.Helper()

	path := filepath.Join(directory, name, "meta.json")
	stored, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(stored, &fields); err != nil {
		t.Fatal(err)
	}
	fields["version"] = json.RawMessage("1")
	fields["data"] = json.RawMessage(`{"workspaceDir":"/tmp/somewhere"}`)

	behind, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, behind, 0o600); err != nil {
		t.Fatal(err)
	}
}
