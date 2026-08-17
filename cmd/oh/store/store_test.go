package store_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"
	"time"

	"crdx.org/io/agent"

	"crdx.org/io/cmd/oh/store"
)

func write(t *testing.T, directory string) string {
	t.Helper()

	log, err := store.Create(directory, store.Header{
		Model:     "gpt-5.6-sol",
		Workspace: "/tmp/somewhere",
		Provider:  "codex",
		Effort:    "high",
		Prompt:    "You are a coding assistant.",
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

	file, err := os.OpenFile(filepath.Join(directory, id+".jsonl"), os.O_WRONLY|os.O_APPEND, 0o600) //nolint:gosec // the path is the test's own
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
	{Kind: agent.Call, ID: "1", Name: "weather", Arguments: `{"city":"London"}`, Render: "London"},
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

	if !slices.Equal(storedSession.Events, conversation) {
		t.Errorf("expected %v, got %v", conversation, storedSession.Events)
	}

	if want := "gpt-5.6-sol"; storedSession.Head.Model != want {
		t.Errorf("expected the model to be pinned to %q, got %q", want, storedSession.Head.Model)
	}

	if storedSession.Head.Effort != "high" {
		t.Errorf("expected the harness settings to survive, got %+v", storedSession.Head)
	}

	if storedSession.Head.Prompt != "You are a coding assistant." {
		t.Errorf("expected the prompt to survive, got %+v", storedSession.Head)
	}

	if len(storedSession.Items) != 1 || string(storedSession.Items[0]) != `{"type":"reasoning"}` {
		t.Errorf("expected the provider's item to come back verbatim, got %v", storedSession.Items)
	}

	if want := "what is the weather in London?"; storedSession.FirstPrompt() != want {
		t.Errorf("expected %q, got %q", want, storedSession.FirstPrompt())
	}
}

// Fixed-width base-62 UUIDv7 IDs sort by creation time.
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

// A harness opened and closed again without a word said is no session, and a directory of empty
// files nothing was ever asked in is a picker full of nothing to resume.
func TestASessionNothingWasSaidInIsNeverWritten(t *testing.T) {
	directory := t.TempDir()

	log, err := store.Create(directory, store.Header{Model: "gpt-5.6-sol"})
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

// The header is written by whatever line reaches the file first, so a session opened by its first
// event is still a session that starts with what it was started as.
func TestTheFirstThingSaidTakesTheHeaderWithIt(t *testing.T) {
	directory := t.TempDir()

	log, err := store.Create(directory, store.Header{Model: "gpt-5.6-sol", Workspace: "/tmp/somewhere"})
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

	if storedSession.Head.Model != "gpt-5.6-sol" || storedSession.Head.Workspace != "/tmp/somewhere" {
		t.Errorf("expected the header to have gone in first, got %+v", storedSession.Head)
	}

	if len(storedSession.Events) != 1 {
		t.Errorf("expected the one event, got %v", storedSession.Events)
	}
}

// The picker says how much was said in a session, which is what was asked and what came back. The
// thinking and the tool calls in between are the working out, and are no part of the count.
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

// A conversation carries on in the file it was stored in, so resuming twice is still one session.
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

// A session killed partway through a line is a session up to the line before it.
func TestAHalfWrittenLineEndsTheSession(t *testing.T) {
	directory := t.TempDir()
	id := write(t, directory)

	appendRaw(t, directory, id, `{"kind":"event","event":{"kind":"te`)

	storedSession, err := store.Read(directory, id)
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(storedSession.Events, conversation) {
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

// The file name carries the time the session was created and nothing else, so the conversation
// added to most recently has to reach the top on its own account.
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
