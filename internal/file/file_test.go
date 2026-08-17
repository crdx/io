package file_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/io/internal/file"
)

func testRoot(t *testing.T, writable *bool) (*file.Root, string) {
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

func TestWritingGoesThroughWhileTheTreeMayBeChanged(t *testing.T) {
	writable := true
	root, directory := testRoot(t, &writable)

	if err := root.WriteFile("made.txt", []byte("content"), 0o644); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(directory, "made.txt")) //nolint:gosec // a path this test made itself
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(data) != "content" {
		t.Errorf("got %q, want %q", data, "content")
	}
}

func TestWritingIsRefusedWhileTheTreeIsReadOnly(t *testing.T) {
	writable := false
	root, directory := testRoot(t, &writable)

	if err := root.WriteFile("made.txt", []byte("content"), 0o644); !errors.Is(err, file.ErrReadOnly) {
		t.Errorf("expected the write to be refused, got %v", err)
	}

	if _, err := os.Stat(filepath.Join(directory, "made.txt")); err == nil {
		t.Error("expected no file to have been made")
	}
}

func TestMakingDirectoriesIsRefusedWhileTheTreeIsReadOnly(t *testing.T) {
	writable := false
	root, directory := testRoot(t, &writable)

	if err := root.MkdirAll("one/two", 0o755); !errors.Is(err, file.ErrReadOnly) {
		t.Errorf("expected the directory to be refused, got %v", err)
	}

	if _, err := os.Stat(filepath.Join(directory, "one")); err == nil {
		t.Error("expected no directory to have been made")
	}
}

// The answer is asked for afresh, so a tree may be closed and opened again while the harness runs.
func TestWhetherTheTreeMayBeChangedIsAskedForEveryCall(t *testing.T) {
	writable := true
	root, _ := testRoot(t, &writable)

	if err := root.WriteFile("one.txt", nil, 0o644); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	writable = false

	if err := root.WriteFile("two.txt", nil, 0o644); !errors.Is(err, file.ErrReadOnly) {
		t.Errorf("expected the write to be refused, got %v", err)
	}

	writable = true

	if err := root.WriteFile("three.txt", nil, 0o644); err != nil {
		t.Errorf("expected the write to be allowed again, got %v", err)
	}
}

// Reading is never withheld, whatever the tree allows.
func TestReadingGoesThroughWhileTheTreeIsReadOnly(t *testing.T) {
	writable := false
	root, directory := testRoot(t, &writable)

	if err := os.WriteFile(filepath.Join(directory, "there.txt"), []byte("content"), 0o600); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if data, err := root.ReadFile("there.txt"); err != nil || string(data) != "content" {
		t.Errorf("got %q and %v", data, err)
	}

	if _, err := root.Stat("there.txt"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	openRoot, err := root.Open("there.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_ = openRoot.Close()

	if data, err := fs.ReadFile(root.FS(), "there.txt"); err != nil || string(data) != "content" {
		t.Errorf("got %q and %v", data, err)
	}
}

func TestTheTreeSaysWhetherItMayBeChanged(t *testing.T) {
	writable := true
	root, directory := testRoot(t, &writable)

	if err := root.RefuseWrite("."); err != nil {
		t.Errorf("expected a writable tree to stand in nothing's way, got %v", err)
	}

	writable = false

	if err := root.RefuseWrite("."); !errors.Is(err, file.ErrReadOnly) {
		t.Errorf("expected a read-only tree to say so, got %v", err)
	}

	if root.Name() != directory {
		t.Errorf("got %q, want %q", root.Name(), directory)
	}
}

func openRoot(t *testing.T) (*os.Root, string) {
	t.Helper()

	directory := t.TempDir()

	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Cleanup(func() { _ = root.Close() })

	return root, directory
}

// The rule is asked about the path, not merely about the tree, so it may turn one name away and let
// the next through.
func TestARefusalNamesThePathItWasAskedAbout(t *testing.T) {
	root, directory := openRoot(t)

	refusal := errors.New("not that one")

	guardedRoot := file.New(root, func(name string) error {
		if strings.Contains(name, "secret") {
			return refusal
		}

		return nil
	})

	if err := guardedRoot.WriteFile("secret.txt", []byte("x"), 0o644); !errors.Is(err, refusal) {
		t.Errorf("expected the write to be refused, got %v", err)
	}

	if err := guardedRoot.WriteFile("plain.txt", []byte("x"), 0o644); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(directory, "plain.txt")) //nolint:gosec // a path this test made itself
	if err != nil || string(data) != "x" {
		t.Errorf("got %q and %v", data, err)
	}
}

// A symlink does not give a refused place an allowed second name.
func TestARefusalAppliesAfterFollowingSymlinks(t *testing.T) {
	root, directory := openRoot(t)

	metadata := filepath.Join(directory, ".git")
	if err := os.Mkdir(metadata, 0o700); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(metadata, "config")
	if err := os.WriteFile(config, []byte("intact"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(".git", "config"), filepath.Join(directory, "alias")); err != nil {
		t.Fatal(err)
	}

	guardedRoot := file.New(root, file.RefuseGitDir)
	if err := guardedRoot.WriteFile("alias", []byte("changed"), 0o600); !errors.Is(err, file.ErrGitDir) {
		t.Errorf("expected the symlink destination to be refused, got %v", err)
	}

	data, err := os.ReadFile(config) //nolint:gosec // a path this test made itself
	if err != nil || string(data) != "intact" {
		t.Errorf("got %q and %v", data, err)
	}
}

// A directory is asked about under its own name, so a write that would make one on the way into a
// refused place is turned away before it starts.
func TestMakingADirectoryIsAskedAboutItsOwnName(t *testing.T) {
	root, directory := openRoot(t)

	guardedRoot := file.New(root, file.RefuseGitDir)

	if err := guardedRoot.MkdirAll(filepath.Join(".git", "hooks"), 0o755); !errors.Is(err, file.ErrGitDir) {
		t.Errorf("expected the directory to be refused, got %v", err)
	}

	if _, err := os.Stat(filepath.Join(directory, ".git")); err == nil {
		t.Error("expected no directory to have been made")
	}
}

// What is refused is a path component rather than a substring, so a name merely ending in .git, or
// beginning with it, is nobody's history and is left alone.
func TestRefuseGitDir(t *testing.T) {
	for _, name := range []string{".git", ".git/config", "a/.git/config", "./.git/HEAD"} {
		if err := file.RefuseGitDir(name); !errors.Is(err, file.ErrGitDir) {
			t.Errorf("expected %q to be refused, got %v", name, err)
		}
	}

	for _, name := range []string{".", "plain.txt", "a.git/config", "git/config", "a/.gitignore"} {
		if err := file.RefuseGitDir(name); err != nil {
			t.Errorf("expected %q to be allowed, got %v", name, err)
		}
	}
}
