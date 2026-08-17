package find_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/io/internal/file"
	"crdx.org/io/toolbox/find"
)

func testRoot(t *testing.T, paths ...string) *file.Root {
	t.Helper()

	directory := t.TempDir()

	for _, path := range paths {
		full := filepath.Join(directory, path)

		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if err := os.WriteFile(full, []byte("content\n"), 0o600); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Cleanup(func() { _ = root.Close() })

	return file.New(root, allowAll)
}

func exec(t *testing.T, root *file.Root, arguments string) (string, error) {
	t.Helper()

	call, err := find.New(root).Parse(arguments)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return call.Exec(t.Context())
}

func TestAGlobMatchesAcrossDirectories(t *testing.T) {
	root := testRoot(t, "main.go", "inner/deep/thing.go", "inner/notes.txt")

	output, err := exec(t, root, `{"pattern":"**/*.go"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := strings.Split(output, "\n")

	if len(found) != 2 {
		t.Fatalf("expected both Go files, got %q", output)
	}

	if !strings.Contains(output, "main.go") || !strings.Contains(output, "thing.go") {
		t.Errorf("expected both Go files, got %q", output)
	}
}

func TestASearchStartsWhereItIsTold(t *testing.T) {
	root := testRoot(t, "main.go", "inner/thing.go")

	output, err := exec(t, root, `{"pattern":"*.go","path":"inner"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output != "inner/thing.go" {
		t.Errorf("expected only the file below inner, got %q", output)
	}
}

func TestTheGitDirectoryIsNotSearched(t *testing.T) {
	root := testRoot(t, ".git/objects/thing.go", "main.go")

	output, err := exec(t, root, `{"pattern":"**/*.go"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(output, ".git") {
		t.Errorf("expected .git to be skipped, got %q", output)
	}
}

func TestASearchThatFindsNothingSaysSo(t *testing.T) {
	root := testRoot(t, "main.go")

	output, err := exec(t, root, `{"pattern":"**/*.rb"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output != "(no matches)" {
		t.Errorf("expected no matches to say so, got %q", output)
	}
}

func TestASearchWithNoPatternIsRefused(t *testing.T) {
	root := testRoot(t, "main.go")

	if _, err := exec(t, root, `{}`); err == nil {
		t.Error("expected a search with no pattern to be refused")
	}
}

func allowAll(string) error { return nil }
