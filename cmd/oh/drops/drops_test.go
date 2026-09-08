package drops

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"crdx.org/io/internal/file"
)

func TestDropsUseADirectoryInsideTheSession(t *testing.T) {
	sessionDirectory := filepath.Join("state", "sessions", "brave-otter")
	want := filepath.Join(sessionDirectory, "drops")
	if got := GetDirectory(sessionDirectory); got != want {
		t.Errorf("drops directory is %q, want %q", got, want)
	}
}

func TestExistingDropsAreMountedReadOnly(t *testing.T) {
	sessionDirectory := t.TempDir()
	directory := GetDirectory(sessionDirectory)
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "image.png"), []byte("image bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	workspaceRoot, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workspaceRoot.Close() }()
	files := file.New(workspaceRoot, func(string) error { return file.ErrReadOnly })
	closeDrops, areDropsMounted, err := Mount(files, sessionDirectory)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = closeDrops() }()
	if !areDropsMounted {
		t.Fatal("drops directory was not mounted")
	}

	path := filepath.Join(directory, "image.png")
	mountedRoot, name, err := files.Resolve(path)
	if err != nil {
		t.Fatal(err)
	}
	content, err := mountedRoot.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "image bytes" {
		t.Errorf("mounted drop reads %q", content)
	}
	if err := mountedRoot.RefuseWrite(name); !errors.Is(err, file.ErrReadOnly) {
		t.Errorf("mounted drop write got %v, want read-only", err)
	}
}

func TestAbsentDropsHaveNothingToMount(t *testing.T) {
	workspaceRoot, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workspaceRoot.Close() }()
	files := file.New(workspaceRoot, func(string) error { return file.ErrReadOnly })

	closeDrops, areDropsMounted, err := Mount(files, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := closeDrops(); err != nil {
		t.Fatal(err)
	}
	if areDropsMounted {
		t.Error("absent drops directory was mounted")
	}
}
