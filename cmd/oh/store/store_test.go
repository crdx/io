package store_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/internal/req"
	"crdx.org/io/session"

	"crdx.org/io/cmd/oh/store"
)

func write(t *testing.T, directory string) string {
	t.Helper()

	log, err := store.Create(directory, store.Meta{
		Model:        "gpt-5.6-sol",
		WorkspaceDir: "/tmp/somewhere",
		Provider:     "codex",
		Effort:       "high",
		Context:      "You are a coding assistant.",
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, event := range conversation {
		if err := log.Event(event); err != nil {
			t.Fatal(err)
		}
	}

	if err := log.Item(json.RawMessage(`{"type":"reasoning"}`)); err != nil {
		t.Fatal(err)
	}

	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	return log.ID()
}

func appendRaw(t *testing.T, directory string, id string, text string) {
	t.Helper()

	file, err := os.OpenFile(filepath.Join(directory, id, "session.jsonl"), os.O_WRONLY|os.O_APPEND, 0o600) //nolint:gosec // the path is the test's own
	if err != nil {
		t.Fatal(err)
	}

	if _, err := file.WriteString(text); err != nil {
		t.Fatal(err)
	}

	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

var conversation = []agent.Event{
	{Kind: agent.Prompt, Text: "what is the weather in London?"},
	{Kind: agent.Text, Text: "Let me look."},
	{Kind: agent.Call, ID: "1", Name: "weather", Arguments: `{"city":"London"}`, Subject: "London"},
	{Kind: agent.Result, ID: "1", Name: "weather", Text: "raining"},
	{Kind: agent.Text, Text: "It is raining."},
}

func TestASessionReadsBackAsItWasWritten(t *testing.T) {
	directory := t.TempDir()
	id := write(t, directory)

	storedSession, err := store.Read(directory, id)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(storedSession.Events, conversation) {
		t.Errorf("expected %v, got %v", conversation, storedSession.Events)
	}

	if want := "gpt-5.6-sol"; storedSession.Meta.Model != want {
		t.Errorf("expected the model to be pinned to %q, got %q", want, storedSession.Meta.Model)
	}

	if storedSession.Meta.Effort != "high" {
		t.Errorf("expected the harness settings to survive, got %+v", storedSession.Meta)
	}

	if storedSession.Meta.Context != "You are a coding assistant." {
		t.Errorf("expected the context to survive, got %+v", storedSession.Meta)
	}

	if len(storedSession.Items) != 1 || string(storedSession.Items[0]) != `{"type":"reasoning"}` {
		t.Errorf("expected the provider's item to come back verbatim, got %v", storedSession.Items)
	}

	if want := "what is the weather in London?"; storedSession.FirstPrompt() != want {
		t.Errorf("expected %q, got %q", want, storedSession.FirstPrompt())
	}
}

func TestTheMetaCanIncludeTheGeneratedSessionID(t *testing.T) {
	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{Context: "before"})
	if err != nil {
		t.Fatal(err)
	}

	context := "scratch=" + log.ID()
	if err := log.SetMeta(store.Meta{Context: context}); err != nil {
		t.Fatal(err)
	}
	if err := log.Event(agent.Event{Kind: agent.Prompt, Text: "store it"}); err != nil {
		t.Fatal(err)
	}
	if err := log.SetMeta(store.Meta{Context: "too late"}); err == nil {
		t.Error("expected a stored meta to be immutable")
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	storedSession, err := store.Read(directory, log.ID())
	if err != nil {
		t.Fatal(err)
	}
	if storedSession.Meta.Context != context {
		t.Errorf("got context %q, want %q", storedSession.Meta.Context, context)
	}
}

func TestASessionIsNamedWithATimeOrderedID(t *testing.T) {
	directory := t.TempDir()

	idPattern := regexp.MustCompile(`^[0-9A-Za-z]{22}$`)

	first := write(t, directory)

	time.Sleep(2 * time.Millisecond)

	second := write(t, directory)

	for _, id := range []string{first, second} {
		if !idPattern.MatchString(id) {
			t.Errorf("expected %q to be 22 alphanumeric digits", id)
		}
	}

	if first >= second {
		t.Errorf("expected %q to sort before %q", first, second)
	}
}

func TestASessionNothingWasSaidInIsNeverWritten(t *testing.T) {
	directory := t.TempDir()

	log, err := store.Create(directory, store.Meta{Model: "gpt-5.6-sol"})
	if err != nil {
		t.Fatal(err)
	}

	if log.ID() == "" {
		t.Error("expected the session to be named before it is written")
	}

	if log.Stored() {
		t.Error("expected nothing to carry on from before anything is said")
	}

	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 0 {
		t.Errorf("expected nothing to have been written, got %v", entries)
	}
}

func TestTheFirstThingSaidTakesTheHeadWithIt(t *testing.T) {
	directory := t.TempDir()

	log, err := store.Create(directory, store.Meta{Model: "gpt-5.6-sol", WorkspaceDir: "/tmp/somewhere"})
	if err != nil {
		t.Fatal(err)
	}

	if err := log.Event(agent.Event{Kind: agent.Prompt, Text: "hello"}); err != nil {
		t.Fatal(err)
	}

	if !log.Stored() {
		t.Error("expected something to carry on from once something has been said")
	}

	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	storedSession, err := store.Read(directory, log.ID())
	if err != nil {
		t.Fatal(err)
	}

	if storedSession.Meta.Model != "gpt-5.6-sol" || storedSession.Meta.WorkspaceDir != "/tmp/somewhere" {
		t.Errorf("expected the meta to have gone in first, got %+v", storedSession.Meta)
	}

	if len(storedSession.Events) != 1 {
		t.Errorf("expected the one event, got %v", storedSession.Events)
	}
}

func TestMessagesCountsWhatWasSaidAndNotTheWorkingOut(t *testing.T) {
	directory := t.TempDir()

	storedSession, err := store.Read(directory, write(t, directory))
	if err != nil {
		t.Fatal(err)
	}

	if want := 3; storedSession.Messages() != want {
		t.Errorf("expected %d messages, got %d", want, storedSession.Messages())
	}
}

func TestAnOpenedSessionKeepsWhatWasThereBefore(t *testing.T) {
	directory := t.TempDir()
	id := write(t, directory)

	log, err := store.Open(directory, id)
	if err != nil {
		t.Fatal(err)
	}

	if err := log.Event(agent.Event{Kind: agent.Prompt, Text: "and tomorrow?"}); err != nil {
		t.Fatal(err)
	}

	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	storedSession, err := store.Read(directory, id)
	if err != nil {
		t.Fatal(err)
	}

	if want := len(conversation) + 1; len(storedSession.Events) != want {
		t.Errorf("expected %d events, got %d", want, len(storedSession.Events))
	}
}

func TestAHalfWrittenLineEndsTheSession(t *testing.T) {
	directory := t.TempDir()
	id := write(t, directory)

	appendRaw(t, directory, id, `{"kind":"event","event":{"kind":"te`)

	storedSession, err := store.Read(directory, id)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(storedSession.Events, conversation) {
		t.Errorf("expected %v, got %v", conversation, storedSession.Events)
	}
}

func TestListReadsTheNewestFirst(t *testing.T) {
	directory := t.TempDir()

	first := write(t, directory)

	time.Sleep(2 * time.Millisecond)

	second := write(t, directory)

	sessions, err := store.List(directory)
	if err != nil {
		t.Fatal(err)
	}

	if len(sessions) != 2 {
		t.Fatalf("expected two sessions, got %d", len(sessions))
	}

	if sessions[0].ID != second || sessions[1].ID != first {
		t.Errorf("expected the newest first, got %s then %s", sessions[0].ID, sessions[1].ID)
	}
}

func TestListPutsTheSessionTouchedLastAtTheTop(t *testing.T) {
	directory := t.TempDir()

	older := write(t, directory)

	time.Sleep(2 * time.Millisecond)
	write(t, directory)
	time.Sleep(2 * time.Millisecond)

	log, err := store.Open(directory, older)
	if err != nil {
		t.Fatal(err)
	}

	if err := log.Event(agent.Event{Kind: agent.Text, Text: "one more thing"}); err != nil {
		t.Fatal(err)
	}

	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	sessions, err := store.List(directory)
	if err != nil {
		t.Fatal(err)
	}

	if sessions[0].ID != older {
		t.Errorf("expected the session added to last at the top, got %s", sessions[0].ID)
	}
}

func TestHTTPObservationCanCreateTheBundleBeforeTheFirstEvent(t *testing.T) {
	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{
		Model: "model", Effort: "high", Provider: "codex", WorkspaceDir: "/workspace",
	})
	if err != nil {
		t.Fatal(err)
	}

	exchange := log.Observer().Start(req.Request{
		Started: time.Now(), Method: "POST", URL: "https://example.com", Protocol: "HTTP/1.1",
	})
	if exchange == nil {
		t.Fatal("expected the HTTP exchange to be recorded")
	}
	if warnings := log.TakeWarnings(); len(warnings) != 0 {
		t.Fatalf("expected no recorder warnings, got %v", warnings)
	}

	if err := log.Event(agent.Event{Kind: agent.Prompt, Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	bundle := filepath.Join(directory, log.ID())
	for _, name := range []string{"session.jsonl", "chat.md", "wire.http"} {
		if _, err := os.Stat(filepath.Join(bundle, name)); err != nil {
			t.Errorf("expected %s: %v", name, err)
		}
	}
}

func TestTheFirstRecordCreatesACompleteBundle(t *testing.T) {
	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{
		Model: "model", Effort: "high", Provider: "codex", WorkspaceDir: "/workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := log.Item(json.RawMessage(`{"type":"first"}`)); err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	bundle := filepath.Join(directory, log.ID())
	for _, name := range []string{"session.jsonl", "chat.md", "wire.http"} {
		info, err := os.Stat(filepath.Join(bundle, name))
		if err != nil {
			t.Errorf("expected %s: %v", name, err)
			continue
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("expected %s mode 0600, got %o", name, info.Mode().Perm())
		}
	}
}

func TestOpeningABundleAppendsToTheMarkdownTranscript(t *testing.T) {
	directory := t.TempDir()
	id := write(t, directory)
	path := filepath.Join(directory, id, "chat.md")
	before, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}

	log, err := store.Open(directory, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := log.Event(agent.Event{Kind: agent.Prompt, Text: "resumed text"}); err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	after, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(after, before) || !strings.Contains(string(after), "resumed text") {
		t.Errorf("expected the transcript to append on resume, got:\n%s", after)
	}
	if strings.Count(string(after), "# Conversation") != 1 {
		t.Errorf("expected one metadata header, got:\n%s", after)
	}
}

func TestAnOpenSessionIsRefusedToASecondWriter(t *testing.T) {
	directory := t.TempDir()
	id := write(t, directory)

	first, err := store.Open(directory, id)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Close() }()

	second, err := store.Open(directory, id)
	if !errors.Is(err, session.ErrInUse) {
		_ = second.Close()
		t.Fatalf("expected the second writer to be refused, got %v", err)
	}
}
