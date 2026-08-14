package edit_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/io/toolbox/edit"
)

func rooted(t *testing.T, content string) (*os.Root, string) {
	t.Helper()

	directory := t.TempDir()

	if err := os.WriteFile(filepath.Join(directory, "a.txt"), []byte(content), 0o600); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Cleanup(func() { _ = root.Close() })

	return root, directory
}

func exec(t *testing.T, root *os.Root, arguments string) (string, error) {
	t.Helper()

	call, err := edit.New(root).Parse(arguments)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return call.Exec()
}

func TestTheTextIsReplacedWhereItAppearsOnce(t *testing.T) {
	root, directory := rooted(t, "one\ntwo\nthree\n")

	if _, err := exec(t, root, `{"path":"a.txt","old_text":"two","new_text":"2"}`); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	edited, err := os.ReadFile(filepath.Join(directory, "a.txt")) //nolint:gosec // a path this test made itself
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(edited) != "one\n2\nthree\n" {
		t.Errorf("expected the replacement, got %q", edited)
	}
}

func TestTextAppearingTwiceIsRefused(t *testing.T) {
	root, directory := rooted(t, "one\none\n")

	_, err := exec(t, root, `{"path":"a.txt","old_text":"one","new_text":"1"}`)
	if err == nil {
		t.Fatal("expected ambiguous text to be refused")
	}

	if !strings.Contains(err.Error(), "more than once") {
		t.Errorf("expected the refusal to say why, got %q", err)
	}

	unchanged, err := os.ReadFile(filepath.Join(directory, "a.txt")) //nolint:gosec // a path this test made itself
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(unchanged) != "one\none\n" {
		t.Errorf("expected the file to be left alone, got %q", unchanged)
	}
}

func TestTextThatIsNotThereIsRefused(t *testing.T) {
	root, _ := rooted(t, "one\n")

	if _, err := exec(t, root, `{"path":"a.txt","old_text":"nine","new_text":"9"}`); err == nil {
		t.Error("expected missing text to be refused")
	}
}

func TestTheFileKeepsItsMode(t *testing.T) {
	root, directory := rooted(t, "one\n")

	if _, err := exec(t, root, `{"path":"a.txt","old_text":"one","new_text":"1"}`); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	info, err := os.Stat(filepath.Join(directory, "a.txt"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if info.Mode().Perm() != 0o600 {
		t.Errorf("expected the mode to survive, got %v", info.Mode().Perm())
	}
}

func TestEditingInsideAGitDirectoryIsRefused(t *testing.T) {
	root, directory := rooted(t, "one\n")

	if err := os.Mkdir(filepath.Join(directory, ".git"), 0o750); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	config := filepath.Join(directory, ".git/config")
	if err := os.WriteFile(config, []byte("one\n"), 0o600); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := exec(t, root, `{"path":".git/config","old_text":"one","new_text":"1"}`); err == nil {
		t.Error("expected an edit inside .git to be refused")
	}
}
