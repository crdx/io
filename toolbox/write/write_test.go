package write_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/io/toolbox/write"
)

func rooted(t *testing.T) (*os.Root, string) {
	t.Helper()

	directory := t.TempDir()

	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Cleanup(func() { _ = root.Close() })

	return root, directory
}

func exec(t *testing.T, root *os.Root, arguments string) (string, error) {
	t.Helper()

	call, err := write.New(root).Parse(arguments)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return call.Exec()
}

func TestAFileIsWrittenWithItsParentDirectories(t *testing.T) {
	root, directory := rooted(t)

	if _, err := exec(t, root, `{"path":"deep/inner/a.txt","content":"hello\n"}`); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	written, err := os.ReadFile(filepath.Join(directory, "deep/inner/a.txt")) //nolint:gosec // a path this test made itself
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(written) != "hello\n" {
		t.Errorf("expected the content, got %q", written)
	}
}

func TestAnExistingFileIsOverwritten(t *testing.T) {
	root, directory := rooted(t)

	if err := os.WriteFile(filepath.Join(directory, "a.txt"), []byte("old\n"), 0o600); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := exec(t, root, `{"path":"a.txt","content":"new\n"}`); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	written, err := os.ReadFile(filepath.Join(directory, "a.txt")) //nolint:gosec // a path this test made itself
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(written) != "new\n" {
		t.Errorf("expected the file to be replaced, got %q", written)
	}
}

func TestWritingInsideAGitDirectoryIsRefused(t *testing.T) {
	root, _ := rooted(t)

	_, err := exec(t, root, `{"path":".git/config","content":"hello\n"}`)
	if err == nil {
		t.Fatal("expected a write inside .git to be refused")
	}

	if !strings.Contains(err.Error(), ".git") {
		t.Errorf("expected the refusal to name .git, got %q", err)
	}
}

func TestAWriteOutsideTheRootIsRefused(t *testing.T) {
	root, _ := rooted(t)

	if _, err := exec(t, root, `{"path":"../escaped.txt","content":"hello\n"}`); err == nil {
		t.Error("expected a write outside the root to be refused")
	}
}

func TestAWriteWithNoPathIsRefused(t *testing.T) {
	root, _ := rooted(t)

	if _, err := exec(t, root, `{"content":"hello\n"}`); err == nil {
		t.Error("expected a write with no path to be refused")
	}
}
