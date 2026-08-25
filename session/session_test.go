package session_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"crdx.org/io/agent"
	"crdx.org/io/session"
)

func TestTheHeadCarriesTheNameAndTheIdentifier(t *testing.T) {
	directory := t.TempDir()

	writer, err := session.Create(directory, nil, nil)
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

func TestMetaCarriesOnlyListingData(t *testing.T) {
	directory := t.TempDir()
	canonicalMeta := json.RawMessage(`{"workspaceDir":"/workspace","systemPrompt":"very large"}`)
	listingData := json.RawMessage(`{"workspaceDir":"/workspace"}`)
	writer, err := session.Create(directory, canonicalMeta, listingData)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "first question"}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Event(agent.Event{Kind: agent.StateChangeEvent, Name: "file_read"}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Event(agent.Event{Kind: agent.ModelMessageEvent, Text: "first answer"}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Item(json.RawMessage(`{"large":"provider state"}`)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	meta, err := session.ReadMeta(directory, writer.Name())
	if err != nil {
		t.Fatal(err)
	}
	if string(meta.Data) != string(listingData) {
		t.Errorf("got meta data %s, want %s", meta.Data, listingData)
	}
	if meta.Title != "first question" || meta.Messages != 2 {
		t.Errorf("unexpected metadata: %+v", meta)
	}
	if meta.Started.IsZero() || meta.Touched.Before(meta.Started) {
		t.Errorf("unexpected metadata times: %+v", meta)
	}

	encoded, err := os.ReadFile(filepath.Join(directory, writer.Name(), "meta.json")) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "systemPrompt") || strings.Contains(string(encoded), "provider state") {
		t.Errorf("meta contains journal-only data: %s", encoded)
	}
	if !strings.Contains(string(encoded), `"data":{"workspaceDir":"/workspace"}`) || strings.Contains(string(encoded), `"meta":`) {
		t.Errorf("meta does not use the caller-owned data field: %s", encoded)
	}
	if !strings.Contains(string(encoded), `"title":"first question"`) || strings.Contains(string(encoded), "firstMessage") {
		t.Errorf("meta does not use the durable title field: %s", encoded)
	}
}

func TestTurnCompletionIsReadBackSeparatelyFromEventsAndItems(t *testing.T) {
	directory := t.TempDir()
	writer, err := session.Create(directory, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "hello"}); err != nil {
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

	storedSession, err := session.Read(directory, writer.Name())
	if err != nil {
		t.Fatal(err)
	}
	if storedSession.TurnCompletions != 1 || len(storedSession.Events) != 1 || len(storedSession.Items) != 1 {
		t.Errorf("unexpected stored session: %+v", storedSession)
	}
}

func TestAResumedSessionContinuesItsMeta(t *testing.T) {
	directory := t.TempDir()
	journalMeta := json.RawMessage(`{"model":"some-model"}`)
	writer, err := session.Create(directory, journalMeta, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "first"}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	resumed, err := session.Open(directory, writer.Name())
	if err != nil {
		t.Fatal(err)
	}
	openedMeta := resumed.JournalMeta()
	if string(openedMeta) != string(journalMeta) {
		t.Errorf("got opened journal meta %s, want %s", openedMeta, journalMeta)
	}
	openedMeta[0] = '!'
	if got := resumed.JournalMeta(); string(got) != string(journalMeta) {
		t.Errorf("opened journal meta changed through its recipient: %s", got)
	}
	if _, err := resumed.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "second"}); err != nil {
		t.Fatal(err)
	}
	if err := resumed.Close(); err != nil {
		t.Fatal(err)
	}

	meta, err := session.ReadMeta(directory, writer.Name())
	if err != nil {
		t.Fatal(err)
	}
	if meta.Title != "first" || meta.Messages != 2 {
		t.Errorf("unexpected resumed metadata: %+v", meta)
	}
}

func TestMetadataAreNewestFirst(t *testing.T) {
	directory := t.TempDir()
	first, err := session.Create(directory, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "first"}); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := session.Create(directory, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "second"}); err != nil {
		t.Fatal(err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}

	metadata, err := session.ListMeta(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(metadata) != 2 || metadata[0].Name != second.Name() || metadata[1].Name != first.Name() {
		t.Errorf("unexpected metadata order: %+v", metadata)
	}
}

func TestMetadataReportMissingMetadata(t *testing.T) {
	directory := t.TempDir()
	writer, err := session.Create(directory, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.EnsureStored(); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(directory, writer.Name(), "meta.json")); err != nil {
		t.Fatal(err)
	}

	if _, err := session.ListMeta(directory); err == nil {
		t.Error("expected the missing metadata to be reported")
	}
}

func TestJournalCarriesMetaEventsAndItems(t *testing.T) {
	directory := t.TempDir()
	meta := json.RawMessage(`{"world":"weather"}`)
	writer, err := session.Create(directory, meta, meta)
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

	writer, err := session.Create(directory, nil, nil)
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

func TestASessionReportsWhetherItIsInUse(t *testing.T) {
	directory := t.TempDir()
	writer := storedSession(t, directory)

	isInUse, err := session.IsInUse(directory, writer.Name())
	if err != nil || !isInUse {
		t.Fatalf("expected the open session to be in use, got %t and %v", isInUse, err)
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	isInUse, err = session.IsInUse(directory, writer.Name())
	if err != nil || isInUse {
		t.Errorf("expected the closed session to be free, got %t and %v", isInUse, err)
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

	writer, err := session.Create(directory, nil, nil)
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
	if entries[0].Format != session.JournalFormat {
		t.Errorf("expected format %d, got %d", session.JournalFormat, entries[0].Format)
	}

	outdated, err := session.Outdated(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(outdated) != 0 {
		t.Errorf("expected a session just written to be current, got %v", outdated)
	}
}

func TestAJournalFromANewerOhIsNamedAndNotReplayed(t *testing.T) {
	directory := t.TempDir()
	name := "brave-otter"

	if err := os.MkdirAll(filepath.Join(directory, name), 0o750); err != nil {
		t.Fatal(err)
	}

	head := fmt.Sprintf(
		`{"kind":"head","time":"2026-08-01T00:00:00Z","version":%d,"id":"one","name":"brave-otter"}`,
		session.JournalFormat+1,
	) + "\n"
	if err := os.WriteFile(filepath.Join(directory, name, "session.jsonl"), []byte(head), 0o600); err != nil {
		t.Fatal(err)
	}

	ahead, err := session.Ahead(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(ahead) != 1 || ahead[0] != name {
		t.Errorf("expected the newer journal to be named as ahead, got %v", ahead)
	}

	outdated, err := session.Outdated(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(outdated) != 0 {
		t.Errorf("expected a newer journal not to count as outdated, got %v", outdated)
	}

	if _, err := session.Read(directory, name); err == nil || !strings.Contains(err.Error(), "newer build") {
		t.Errorf("expected reading a newer journal to say where it came from, got %v", err)
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

func TestMetadataIsStampedWithItsFormat(t *testing.T) {
	directory := t.TempDir()
	writer, err := session.Create(directory, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.EnsureStored(); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	meta, err := session.ReadMeta(directory, writer.Name())
	if err != nil {
		t.Fatal(err)
	}
	if meta.Version != session.MetaFormat {
		t.Errorf("expected metadata stamped %d, got %d", session.MetaFormat, meta.Version)
	}
}

func TestMetadataInAnotherFormatIsNamedAsSuch(t *testing.T) {
	directory := t.TempDir()
	writer, err := session.Create(directory, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.EnsureStored(); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(directory, writer.Name(), "meta.json")
	stored, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(stored, &fields); err != nil {
		t.Fatal(err)
	}
	fields["version"] = json.RawMessage(strconv.Itoa(session.MetaFormat + 1))
	ahead, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, ahead, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := session.ReadMeta(directory, writer.Name()); !errors.Is(err, session.ErrMetaOutOfDate) {
		t.Errorf("expected metadata in another format to be named as such, got %v", err)
	}
}
