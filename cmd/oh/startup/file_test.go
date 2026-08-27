package startup

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareInitialFilesCopiesFilesIntoTheScratchDirectoryAndListsThem(t *testing.T) {
	sourceDirectory := t.TempDir()
	sourcePaths := []string{
		filepath.Join(sourceDirectory, "brief.md"),
		filepath.Join(sourceDirectory, "notes.txt"),
	}
	for i, sourcePath := range sourcePaths {
		if err := os.WriteFile(sourcePath, fmt.Appendf(nil, "file %d\n", i), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	scratchDirectory := t.TempDir()

	message, err := PrepareInitialFiles(sourcePaths, scratchDirectory)
	if err != nil {
		t.Fatal(err)
	}

	for i, name := range []string{"brief.md", "notes.txt"} {
		copied, err := os.ReadFile(filepath.Join(scratchDirectory, name)) //nolint:gosec // the test's own path
		if err != nil {
			t.Fatal(err)
		}
		want := fmt.Sprintf("file %d\n", i)
		if string(copied) != want {
			t.Errorf("got copied content %q, want %q", copied, want)
		}
	}

	wantMessage := fmt.Sprintf(
		"%s\n\n- %s\n- %s",
		messageIntroduction,
		filepath.Join(scratchDirectory, "brief.md"),
		filepath.Join(scratchDirectory, "notes.txt"),
	)
	if message != wantMessage {
		t.Errorf("got message %q, want %q", message, wantMessage)
	}
}

func TestPrepareInitialFilesReportsAnUnreadableFile(t *testing.T) {
	_, err := PrepareInitialFiles([]string{filepath.Join(t.TempDir(), "missing.md")}, t.TempDir())
	if err == nil {
		t.Fatal("expected an error")
	}
}
