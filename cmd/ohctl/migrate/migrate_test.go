package migrate_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/io/cmd/ohctl/migrate"
	"crdx.org/io/session"
)

func storedJournal(t *testing.T, lines ...string) (string, string) {
	t.Helper()

	directory := t.TempDir()
	name := "brave-otter"

	if err := os.MkdirAll(filepath.Join(directory, name), 0o750); err != nil {
		t.Fatal(err)
	}

	body := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(directory, name, "session.jsonl"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	return directory, name
}

func options(directory string) migrate.Options {
	return migrate.Options{Directory: directory, BackupDir: directory + "_copies"}
}

func dryRun(directory string) migrate.Options {
	held := options(directory)
	held.DryRun = true

	return held
}

func journalLines(t *testing.T, directory string, name string) []map[string]json.RawMessage {
	t.Helper()

	body, err := os.ReadFile(filepath.Join(directory, name, "session.jsonl")) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}

	var lines []map[string]json.RawMessage

	for text := range strings.SplitSeq(strings.TrimSpace(string(body)), "\n") {
		var line map[string]json.RawMessage
		if err := json.Unmarshal([]byte(text), &line); err != nil {
			t.Fatal(err)
		}

		lines = append(lines, line)
	}

	return lines
}

func TestAJournalWithoutAVersionIsMigratedFromTheFirstFormat(t *testing.T) {
	directory, name := storedJournal(t,
		`{"kind":"head","time":"2026-08-01T00:00:00Z","id":"one","name":"brave-otter"}`,
		`{"kind":"event","time":"2026-08-01T00:00:01Z","event":{"kind":"tool_call_request","name":"read","highlight":{"kind":"focus","value":"draw.go"}}}`,
	)

	from, err := migrate.Session(options(directory), name)
	if err != nil {
		t.Fatal(err)
	}

	if from != 1 {
		t.Errorf("expected an unnumbered journal to count as the first format, got %d", from)
	}

	lines := journalLines(t, directory, name)

	if got := string(lines[0]["version"]); got != "2" {
		t.Errorf("expected the head to say format 2, got %q", got)
	}

	event := string(lines[1]["event"])
	if strings.Contains(event, "highlight") || !strings.Contains(event, `"emphasis"`) {
		t.Errorf("expected highlight to have become emphasis, got %s", event)
	}
	if !strings.Contains(event, `"value":"draw.go"`) {
		t.Errorf("expected what the field said to survive the rename, got %s", event)
	}
}

func TestAJournalAlreadyCurrentIsLeftAlone(t *testing.T) {
	head := `{"kind":"head","time":"2026-08-01T00:00:00Z","version":2,"id":"one","name":"brave-otter"}`
	directory, name := storedJournal(t, head)

	from, err := migrate.Session(options(directory), name)
	if err != nil {
		t.Fatal(err)
	}

	if from != session.Format {
		t.Errorf("expected the current format, got %d", from)
	}

	if got := string(journalLines(t, directory, name)[0]["id"]); got != `"one"` {
		t.Errorf("expected the journal untouched, got %s", got)
	}
}

func TestADryRunWritesNothing(t *testing.T) {
	directory, name := storedJournal(t,
		`{"kind":"head","time":"2026-08-01T00:00:00Z","id":"one","name":"brave-otter"}`,
		`{"kind":"event","time":"2026-08-01T00:00:01Z","event":{"kind":"tool_call_request","highlight":{"kind":"focus"}}}`,
	)

	if _, err := migrate.Session(dryRun(directory), name); err != nil {
		t.Fatal(err)
	}

	lines := journalLines(t, directory, name)
	if _, numbered := lines[0]["version"]; numbered {
		t.Error("expected a dry run to leave the head unnumbered")
	}
	if !strings.Contains(string(lines[1]["event"]), "highlight") {
		t.Error("expected a dry run to leave the event as it found it")
	}
}

func TestAJournalFromANewerBuildIsRefused(t *testing.T) {
	directory, name := storedJournal(t,
		`{"kind":"head","time":"2026-08-01T00:00:00Z","version":99,"id":"one","name":"brave-otter"}`,
	)

	_, err := migrate.Session(options(directory), name)
	if err == nil {
		t.Fatal("expected a journal from the future to be refused")
	}

	if !strings.Contains(err.Error(), "newer oh") {
		t.Errorf("expected the error to say where it came from, got %v", err)
	}
}

func TestAnEmptyJournalIsRefused(t *testing.T) {
	directory := t.TempDir()
	name := "brave-otter"

	if err := os.MkdirAll(filepath.Join(directory, name), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, name, "session.jsonl"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := migrate.Session(options(directory), name); err == nil {
		t.Fatal("expected an empty journal to be refused")
	}
}

func TestACopyOfTheBundleIsKeptBeforeItIsWritten(t *testing.T) {
	directory, name := storedJournal(t,
		`{"kind":"head","time":"2026-08-01T00:00:00Z","id":"one","name":"brave-otter"}`,
		`{"kind":"event","time":"2026-08-01T00:00:01Z","event":{"kind":"tool_call_request","highlight":{"kind":"focus"}}}`,
	)

	held := options(directory)

	if _, err := migrate.Session(held, name); err != nil {
		t.Fatal(err)
	}

	kept, err := os.ReadFile(filepath.Join(held.BackupDir, name, "session.jsonl")) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(kept), `"highlight"`) {
		t.Error("expected the copy to hold the journal as it stood before")
	}
}

func TestACopyIsNotWrittenOver(t *testing.T) {
	directory, name := storedJournal(t,
		`{"kind":"head","time":"2026-08-01T00:00:00Z","id":"one","name":"brave-otter"}`,
	)

	held := options(directory)
	if err := os.MkdirAll(filepath.Join(held.BackupDir, name), 0o750); err != nil {
		t.Fatal(err)
	}

	_, err := migrate.Session(held, name)
	if err == nil {
		t.Fatal("expected a copy already kept to stop the migration")
	}

	if !strings.Contains(err.Error(), "move it aside") {
		t.Errorf("expected the error to say what to do, got %v", err)
	}
}

func TestADryRunKeepsNoCopy(t *testing.T) {
	directory, name := storedJournal(t,
		`{"kind":"head","time":"2026-08-01T00:00:00Z","id":"one","name":"brave-otter"}`,
	)

	held := dryRun(directory)
	if _, err := migrate.Session(held, name); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(held.BackupDir); !os.IsNotExist(err) {
		t.Error("expected a dry run to leave no copies behind")
	}
}

func TestTheTranscriptIsWrittenAgainFromTheCarriedJournal(t *testing.T) {
	directory, name := storedJournal(t,
		`{"kind":"head","time":"2026-08-01T00:00:00Z","id":"one","name":"brave-otter"}`,
		`{"kind":"event","time":"2026-08-01T00:00:01Z","event":{"kind":"tool_call_request","name":"read","render":"draw.go","highlight":{"kind":"focus","value":"draw.go"}}}`,
	)

	held := options(directory)

	stale := filepath.Join(directory, name, "chat.md")
	if err := os.WriteFile(stale, []byte("# what it used to say\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := migrate.Session(held, name); err != nil {
		t.Fatal(err)
	}

	written, err := os.ReadFile(stale) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(string(written), "what it used to say") {
		t.Error("expected the transcript to be written again rather than left as it was")
	}

	if !strings.Contains(string(written), "Emphasis") {
		t.Errorf("expected the transcript to say what the migrated journal says, got %s", written)
	}
}
