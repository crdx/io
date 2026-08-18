package write_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/io/internal/file"
	"crdx.org/io/toolbox/write"
)

func testRoot(t *testing.T) (*file.Root, string) {
	t.Helper()

	directory := t.TempDir()

	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Cleanup(func() { _ = root.Close() })

	return file.New(root, allowAll), directory
}

func exec(t *testing.T, root *file.Root, arguments string) (string, error) {
	t.Helper()

	call, err := write.New(root).Parse(arguments)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return call.Exec(t.Context())
}

func TestAFileIsWrittenWithItsParentDirectories(t *testing.T) {
	root, directory := testRoot(t)

	if _, err := exec(t, root, `{"path":"deep/inner/a.txt","content":"hello\n"}`); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	contents, err := os.ReadFile(filepath.Join(directory, "deep/inner/a.txt")) //nolint:gosec // a path this test made itself
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(contents) != "hello\n" {
		t.Errorf("expected the content, got %q", contents)
	}
}

func TestWriteSizesUseCompactBytes(t *testing.T) {
	content := strings.Repeat("x", 1536)
	path, size := write.Render(write.Args{Path: "result.txt", Content: content})
	if path != "result.txt" || size != "1.5K" {
		t.Errorf("got path %q and size %q, want result.txt and 1.5K", path, size)
	}

	root, _ := testRoot(t)
	result, err := exec(t, root, `{"path":"result.txt","content":"`+content+`"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "wrote 1.5K to result.txt" {
		t.Errorf("got %q, want compact write result", result)
	}
}

func TestAnExistingFileIsOverwritten(t *testing.T) {
	root, directory := testRoot(t)

	if err := os.WriteFile(filepath.Join(directory, "a.txt"), []byte("old\n"), 0o600); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := exec(t, root, `{"path":"a.txt","content":"new\n"}`); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	contents, err := os.ReadFile(filepath.Join(directory, "a.txt")) //nolint:gosec // a path this test made itself
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(contents) != "new\n" {
		t.Errorf("expected the file to be replaced, got %q", contents)
	}
}

func TestWritingInsideAGitDirectoryIsRefused(t *testing.T) {
	root, _ := gitGuardedRoot(t)

	_, err := exec(t, root, `{"path":".git/config","content":"hello\n"}`)
	if err == nil {
		t.Fatal("expected a write inside .git to be refused")
	}

	if !strings.Contains(err.Error(), ".git") {
		t.Errorf("expected the refusal to name .git, got %q", err)
	}
}

func TestAWriteOutsideTheRootIsRefused(t *testing.T) {
	root, _ := testRoot(t)

	if _, err := exec(t, root, `{"path":"../escaped.txt","content":"hello\n"}`); err == nil {
		t.Error("expected a write outside the root to be refused")
	}
}

func TestAMountedRootFollowsItsWriteMode(t *testing.T) {
	root, _ := testRoot(t)
	writable := false
	tmp, directory := switchableRoot(t, &writable)
	root.Mount("/tmp", tmp)

	if _, err := exec(t, root, `{"path":"/tmp/result.txt","content":"answer\n"}`); !errors.Is(err, file.ErrReadOnly) {
		t.Errorf("expected the write to be refused, got %v", err)
	}

	writable = true
	if _, err := exec(t, root, `{"path":"/tmp/result.txt","content":"answer\n"}`); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	contents, err := os.ReadFile(filepath.Join(directory, "result.txt")) //nolint:gosec // a path this test made itself
	if err != nil || string(contents) != "answer\n" {
		t.Errorf("got %q and %v", contents, err)
	}
}

func TestAWriteWithNoPathIsRefused(t *testing.T) {
	root, _ := testRoot(t)

	if _, err := exec(t, root, `{"content":"hello\n"}`); err == nil {
		t.Error("expected a write with no path to be refused")
	}
}

func allowAll(string) error { return nil }

func gitGuardedRoot(t *testing.T) (*file.Root, string) {
	t.Helper()

	directory := t.TempDir()

	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Cleanup(func() { _ = root.Close() })

	return file.New(root, file.RefuseGitDir), directory
}

func switchableRoot(t *testing.T, writable *bool) (*file.Root, string) {
	t.Helper()

	directory := t.TempDir()

	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Cleanup(func() { _ = root.Close() })

	return file.New(root, func(string) error {
		if *writable {
			return nil
		}

		return file.ErrReadOnly
	}), directory
}

func TestWritingIsRefusedWhileTheTreeIsReadOnly(t *testing.T) {
	writable := false
	root, directory := switchableRoot(t, &writable)

	if _, err := exec(t, root, `{"path":"made.txt","content":"x"}`); !errors.Is(err, file.ErrReadOnly) {
		t.Errorf("expected the write to be refused, got %v", err)
	}

	if _, err := os.Stat(filepath.Join(directory, "made.txt")); err == nil {
		t.Error("expected no file to have been made")
	}
}

func TestTheToolChangesNothingWhileTheTreeIsReadOnly(t *testing.T) {
	writable := false
	root, _ := switchableRoot(t, &writable)

	built := write.New(root)

	if !built.ReadOnly() {
		t.Error("expected a tool over a read-only tree to change nothing")
	}

	writable = true

	if built.ReadOnly() {
		t.Error("expected a tool over a writable tree to say it writes")
	}
}
