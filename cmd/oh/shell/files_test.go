package shell_test

import (
	"os"
	"path/filepath"
	"testing"

	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/shell"
	"crdx.org/io/internal/file"
)

func TestHomeMountIsReadableByFileTools(t *testing.T) {
	workspaceRoot, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workspaceRoot.Close() }()

	files := file.New(workspaceRoot, func(string) error { return file.ErrReadOnly })
	home := t.TempDir()
	path := filepath.Join(home, "reference")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	homeRoot, err := shell.MountHomeDirectory(files, home, caps.NewMode(caps.Read))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = homeRoot.Close() }()

	resolvedRoot, name, err := files.Resolve(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := resolvedRoot.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Errorf("got %q, want hello", data)
	}
}

func TestTemporaryMountIsWritableWithoutAShell(t *testing.T) {
	workspaceRoot, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workspaceRoot.Close() }()

	files := file.New(workspaceRoot, func(string) error { return file.ErrReadOnly })
	temporaryDirectory := t.TempDir()
	temporaryRoot, err := shell.MountTemporaryDirectory(files, temporaryDirectory)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = temporaryRoot.Close() }()

	resolvedRoot, name, err := files.Resolve("/tmp/proof")
	if err != nil {
		t.Fatal(err)
	}
	if err := resolvedRoot.WriteFile(name, []byte("written"), 0o600); err != nil {
		t.Fatalf("tmp was not writable: %v", err)
	}
}
