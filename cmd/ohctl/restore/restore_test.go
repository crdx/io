package restore

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/ohctl/console"
	"crdx.org/io/session"
)

var updateGoldens = flag.Bool("update", false, "write what was drawn back to the golden files")

func TestUsageMatchesTheGolden(t *testing.T) {
	assertGolden(t, "usage.txt", strings.ReplaceAll(usage, "$0", "ohctl"))
}

func assertGolden(t *testing.T, name string, drawn string) {
	t.Helper()

	goldenPath := filepath.Join("testdata", name)
	if *updateGoldens {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, []byte(drawn), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath) //nolint:gosec // fixed testdata path
	if err != nil {
		t.Fatal(err)
	}
	if drawn != string(want) {
		t.Errorf("output differs from %s\n--- got ---\n%s--- want ---\n%s", goldenPath, drawn, want)
	}
}

func archivedSession(t *testing.T, directory string) string {
	t.Helper()

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
	if err := session.Archive(directory, writer.Name()); err != nil {
		t.Fatal(err)
	}

	return writer.Name()
}

func TestAnArchivedSessionIsUnpackedAndNamedOnTheScreen(t *testing.T) {
	directory := t.TempDir()
	name := archivedSession(t, directory)

	var screen, failure strings.Builder
	err := run(directory, []string{name}, console.Output{Screen: &screen, Failure: &failure})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(screen.String(), name) {
		t.Errorf("expected the restored session to be named, got %q", screen.String())
	}
	if session.IsArchived(directory, name) {
		t.Error("expected the archive to be gone")
	}
	if _, err := session.Read(directory, name); err != nil {
		t.Errorf("expected the session to be readable again, got %v", err)
	}
}

func TestEveryRefusalMatchesTheGolden(t *testing.T) {
	directory := t.TempDir()

	var screen, failure strings.Builder
	err := run(
		directory,
		[]string{"absent-heron", "../elsewhere"},
		console.Output{Screen: &screen, Failure: &failure},
	)
	if err == nil {
		t.Fatal("expected the refusals to be counted")
	}

	assertGolden(t, "refusals.txt", strings.Join([]string{
		"=== screen ===\n", screen.String(),
		"=== failure ===\n", failure.String(),
		"=== error ===\n", err.Error(), "\n",
	}, ""))
}

func TestASessionThatWasNeverArchivedIsReportedAndCounted(t *testing.T) {
	directory := t.TempDir()
	name := archivedSession(t, directory)

	var screen, failure strings.Builder
	err := run(directory, []string{name, "absent-heron"}, console.Output{Screen: &screen, Failure: &failure})

	if err == nil || err.Error() != "1 of 2 could not be restored" {
		t.Fatalf("got %v", err)
	}
	if !strings.Contains(failure.String(), "absent-heron") {
		t.Errorf("expected the missing session to be named, got %q", failure.String())
	}
}
