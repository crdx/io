package sessions

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/io/agent"

	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/store"
)

func TestAResumedConversationOpensInTheModeItWasLeftIn(t *testing.T) {
	leftCaps := caps.Read | caps.Git
	assumedCaps := caps.Read | caps.Write
	resumedSession := &store.Session{Events: []agent.Event{caps.ModeEvent(leftCaps)}}

	got, err := OpeningCaps(assumedCaps, false, resumedSession)
	if err != nil || got != leftCaps {
		t.Errorf("expected %s, got %s and %v", leftCaps.Flags(), got.Flags(), err)
	}

	got, err = OpeningCaps(assumedCaps, false, &store.Session{})
	if err != nil || got != assumedCaps {
		t.Errorf("expected a silent session to leave %s alone, got %s and %v", assumedCaps.Flags(), got.Flags(), err)
	}

	got, err = OpeningCaps(assumedCaps, false, nil)
	if err != nil || got != assumedCaps {
		t.Errorf("expected a fresh conversation to keep %s, got %s and %v", assumedCaps.Flags(), got.Flags(), err)
	}
}

func TestAResumedConversationCannotBeAskedForAnotherMode(t *testing.T) {
	leftCaps := caps.Read | caps.Git
	resumedSession := &store.Session{Events: []agent.Event{caps.ModeEvent(leftCaps)}}

	if _, err := OpeningCaps(caps.Read|caps.Shell, true, resumedSession); err == nil {
		t.Error("expected another mode to be refused")
	}

	got, err := OpeningCaps(leftCaps, true, resumedSession)
	if err != nil || got != leftCaps {
		t.Errorf("expected the mode it was left in to be allowed, got %s and %v", got.Flags(), err)
	}
}

func TestLoadingACrashedSessionIsRefusedWithoutChangingItsJournal(t *testing.T) {
	directory := t.TempDir()
	workspaceDir := t.TempDir()
	writer, err := store.Create(directory, store.Meta{WorkspaceDir: workspaceDir})
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

	path := filepath.Join(directory, name, "session.jsonl")
	before, err := os.ReadFile(path) //nolint:gosec // the test's own session
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LoadForResume(directory, workspaceDir, name); err == nil || !strings.Contains(err.Error(), "did not finish every turn") {
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

func TestGettingAForkSourcePreparesTheNewConversation(t *testing.T) {
	directory := t.TempDir()
	workspaceDirectory := t.TempDir()
	writer, err := store.Create(directory, store.Meta{WorkspaceDir: workspaceDirectory})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = writer.Close() }()

	if err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "begin"}); err != nil {
		t.Fatal(err)
	}

	forkSource, err := GetForkSource(directory, workspaceDirectory, writer.Name(), "focus on tests")
	if err != nil {
		t.Fatal(err)
	}
	wantInitialFilePath := filepath.Join(directory, writer.Name(), "chat.md")
	if forkSource.InitialFilePath != wantInitialFilePath {
		t.Errorf("got initial file %q, want %q", forkSource.InitialFilePath, wantInitialFilePath)
	}
	wantMessage := forkSourcePrompt + "\n\nfocus on tests"
	if forkSource.InitialUserMessage != wantMessage {
		t.Errorf("got message %q, want %q", forkSource.InitialUserMessage, wantMessage)
	}

	forkSource, err = GetForkSource(directory, workspaceDirectory, writer.Name(), "")
	if err != nil {
		t.Fatal(err)
	}
	if forkSource.InitialUserMessage != forkSourcePrompt {
		t.Errorf("got default message %q, want %q", forkSource.InitialUserMessage, forkSourcePrompt)
	}
}

func TestLoadingARunningSessionReportsThatItIsRunning(t *testing.T) {
	directory := t.TempDir()
	workspaceDir := t.TempDir()
	writer, err := store.Create(directory, store.Meta{WorkspaceDir: workspaceDir})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = writer.Close() }()

	if err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "begin"}); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadForResume(directory, workspaceDir, writer.Name()); err == nil || !strings.Contains(err.Error(), "is running") {
		t.Fatalf("expected the running session to be identified, got %v", err)
	}
}

func TestLoadingACompletedSessionSucceeds(t *testing.T) {
	directory := t.TempDir()
	workspaceDir := t.TempDir()
	writer, err := store.Create(directory, store.Meta{WorkspaceDir: workspaceDir})
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

	if _, err := LoadForResume(directory, workspaceDir, writer.Name()); err != nil {
		t.Fatalf("expected the completed session to load: %v", err)
	}

	if _, err := LoadForResume(directory, t.TempDir(), writer.Name()); err == nil ||
		!strings.Contains(err.Error(), "can only be opened there") {
		t.Fatalf("expected a session of another workspace to be refused, got %v", err)
	}
}

func TestAForkSourceOfAnotherWorkspaceIsRefused(t *testing.T) {
	directory := t.TempDir()
	writer, err := store.Create(directory, store.Meta{WorkspaceDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = writer.Close() }()

	if err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "begin"}); err != nil {
		t.Fatal(err)
	}

	if _, err := GetForkSource(directory, t.TempDir(), writer.Name(), ""); err == nil ||
		!strings.Contains(err.Error(), "can only be opened there") {
		t.Fatalf("expected a session of another workspace to be refused, got %v", err)
	}
}

func TestNamingNoSessionResumesNothing(t *testing.T) {
	resumedSession, err := LoadForResume(t.TempDir(), t.TempDir(), "")
	if err != nil || resumedSession != nil {
		t.Errorf("expected nothing to resume, got %v and %v", resumedSession, err)
	}
}

func TestARunningSessionIsRefused(t *testing.T) {
	directory := t.TempDir()
	writer, err := store.Create(directory, store.Meta{WorkspaceDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Item(json.RawMessage(`{"role":"user"}`)); err != nil {
		t.Fatal(err)
	}
	if err := writer.CompleteTurn(); err != nil {
		t.Fatal(err)
	}

	resumedSession := &store.Session{Name: writer.Name()}
	if _, err := OpenWriter(directory, resumedSession, store.Meta{}); err == nil || !strings.Contains(err.Error(), "is running") {
		t.Fatalf("expected the running session to be refused, got %v", err)
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestResumeCommandNamesTheBinaryAndSession(t *testing.T) {
	if got := ResumeCommand("/usr/local/bin/oh", "able-dolphin"); got != "oh -r able-dolphin" {
		t.Errorf("got %q", got)
	}
}
