package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"crdx.org/io/internal/file"
)

func TestConfiguredPathsAreMountedWithTheirRequestedFileAccess(t *testing.T) {
	workspace := t.TempDir()
	workspaceRoot, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workspaceRoot.Close() }()

	mode := NewMode(capRead)
	files := file.New(workspaceRoot, refuseWrite(mode))
	readDirectory := t.TempDir()
	writeDirectory := t.TempDir()
	execDirectory := t.TempDir()
	if err := os.Mkdir(filepath.Join(writeDirectory, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(readDirectory, "reference"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	roots, err := mountConfiguredPaths(files, mode, configuredPaths{
		Read: []string{readDirectory}, Write: []string{writeDirectory}, Exec: []string{execDirectory},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer closeConfiguredRoots(roots)

	readRoot, name, err := files.Resolve(filepath.Join(readDirectory, "reference"))
	if err != nil {
		t.Fatal(err)
	}
	if data, err := readRoot.ReadFile(name); err != nil || string(data) != "hello" {
		t.Errorf("read got %q and %v", data, err)
	}
	if err := readRoot.WriteFile(name, []byte("changed"), 0o600); !errors.Is(err, file.ErrReadOnly) {
		t.Errorf("read path write got %v, want read-only", err)
	}

	writeRoot, name, err := files.Resolve(filepath.Join(writeDirectory, "output"))
	if err != nil {
		t.Fatal(err)
	}
	if err := writeRoot.WriteFile(name, []byte("blocked"), 0o600); !errors.Is(err, file.ErrReadOnly) {
		t.Errorf("write path without write capability got %v, want read-only", err)
	}
	mode.Toggle(capWrite)
	if err := writeRoot.WriteFile(name, []byte("written"), 0o600); err != nil {
		t.Fatalf("write path with write capability: %v", err)
	}
	if err := writeRoot.WriteFile(filepath.Join(".git", "config"), nil, 0o600); !errors.Is(err, file.ErrGitDir) {
		t.Errorf("repository metadata write got %v, want git refusal", err)
	}
	mode.Toggle(capGit)
	if err := writeRoot.WriteFile(filepath.Join(".git", "config"), nil, 0o600); err != nil {
		t.Errorf("repository metadata write with git capability: %v", err)
	}

	if _, _, err := files.Resolve(execDirectory); !errors.Is(err, file.ErrOutsideRoot) {
		t.Errorf("exec-only path resolved through file tools with %v", err)
	}
}
