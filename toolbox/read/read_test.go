package read_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/io/toolbox/read"
)

func rooted(t *testing.T, name string, content string) *os.Root {
	t.Helper()

	directory := t.TempDir()

	if err := os.WriteFile(filepath.Join(directory, name), []byte(content), 0o600); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Cleanup(func() { _ = root.Close() })

	return root
}

func exec(t *testing.T, root *os.Root, arguments string) (string, error) {
	t.Helper()

	call, err := read.New(root).Parse(arguments)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return call.Exec()
}

func TestAFileWithNoRangeComesBackWhole(t *testing.T) {
	root := rooted(t, "notes.txt", "one\ntwo\nthree\n")

	output, err := exec(t, root, `{"path":"notes.txt"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output != "one\ntwo\nthree\n" {
		t.Errorf("expected the whole file, got %q", output)
	}
}

func TestALineRangeComesBackOnItsOwn(t *testing.T) {
	root := rooted(t, "notes.txt", "one\ntwo\nthree\nfour\n")

	output, err := exec(t, root, `{"path":"notes.txt","offset":2,"limit":2}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output != "two\nthree" {
		t.Errorf("expected the second and third lines, got %q", output)
	}
}

func TestAnOffsetPastTheEndSaysHowLongTheFileIs(t *testing.T) {
	root := rooted(t, "notes.txt", "one\ntwo\n")

	_, err := exec(t, root, `{"path":"notes.txt","offset":9}`)
	if err == nil {
		t.Fatal("expected an offset past the end to be refused")
	}

	if !strings.Contains(err.Error(), "2 lines") {
		t.Errorf("expected the length to be named, got %q", err)
	}
}

func TestAHugeFileIsNotCutShort(t *testing.T) {
	content := strings.Repeat("a line of text\n", 8000)
	root := rooted(t, "big.txt", content)

	output, err := exec(t, root, `{"path":"big.txt"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output != content {
		t.Errorf("expected %d bytes, got %d", len(content), len(output))
	}
}

func TestAPathOutsideTheRootIsRefused(t *testing.T) {
	root := rooted(t, "notes.txt", "one\n")

	if _, err := exec(t, root, `{"path":"../../etc/passwd"}`); err == nil {
		t.Error("expected a path outside the root to be refused")
	}
}

func TestAReadWithNoPathIsRefused(t *testing.T) {
	root := rooted(t, "notes.txt", "one\n")

	if _, err := exec(t, root, `{}`); err == nil {
		t.Error("expected a read with no path to be refused")
	}
}

func TestRenderSaysWhichLinesAreBeingRead(t *testing.T) {
	rendered := read.Render(read.Args{Path: "notes.txt", Offset: 10, Limit: 5})

	if rendered != "notes.txt:10-14" {
		t.Errorf("expected the range, got %q", rendered)
	}
}
