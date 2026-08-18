package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"crdx.org/io/internal/file"
)

func configuredPathTestRoot(t *testing.T, mode *Mode) *file.Root {
	t.Helper()

	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return file.New(root, refuseWrite(mode))
}

func TestMissingConfiguredPathsAreWarnedAboutAndSkipped(t *testing.T) {
	existingRead := filepath.Join(t.TempDir(), "read")
	if err := os.WriteFile(existingRead, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	existingWrite := t.TempDir()
	existingExec := filepath.Join(t.TempDir(), "exec")
	if err := os.WriteFile(existingExec, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	missingRead := filepath.Join(t.TempDir(), "missing-read")
	missingWrite := filepath.Join(t.TempDir(), "missing-write")
	missingExec := filepath.Join(t.TempDir(), "missing-exec")

	var warnings strings.Builder
	filtered, err := keepExistingConfiguredPaths(configuredPaths{
		Read:  []string{existingRead, missingRead},
		Write: []string{existingWrite, missingWrite},
		Exec:  []string{existingExec, missingExec},
	}, &warnings)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(filtered.Read, []string{existingRead}) {
		t.Errorf("got read paths %v, want only %s", filtered.Read, existingRead)
	}
	if !slices.Equal(filtered.Write, []string{existingWrite}) {
		t.Errorf("got write paths %v, want only %s", filtered.Write, existingWrite)
	}
	if !slices.Equal(filtered.Exec, []string{existingExec}) {
		t.Errorf("got executable paths %v, want only %s", filtered.Exec, existingExec)
	}
	for _, path := range []string{missingRead, missingWrite, missingExec} {
		if !strings.Contains(warnings.String(), "warning: could not mount configured path "+path) {
			t.Errorf("warning does not name missing path %s: %q", path, warnings.String())
		}
	}
}

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

func TestConfiguredFilesAreMountedWithoutTheirSiblings(t *testing.T) {
	mode := NewMode(capRead)
	files := configuredPathTestRoot(t, mode)
	directory := t.TempDir()
	readPath := filepath.Join(directory, "reference")
	writePath := filepath.Join(directory, "output")
	siblingPath := filepath.Join(directory, "private")
	if err := os.WriteFile(readPath, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(writePath, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}

	roots, err := mountConfiguredPaths(files, mode, configuredPaths{
		Read: []string{readPath, writePath}, Write: []string{writePath},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer closeConfiguredRoots(roots)

	readRoot, name, err := files.Resolve(readPath)
	if err != nil {
		t.Fatal(err)
	}
	if data, err := readRoot.ReadFile(name); err != nil || string(data) != "hello" {
		t.Errorf("read got %q and %v", data, err)
	}
	if err := readRoot.WriteFile(name, []byte("changed"), 0o600); !errors.Is(err, file.ErrReadOnly) {
		t.Errorf("read file write got %v, want read-only", err)
	}

	writeRoot, name, err := files.Resolve(writePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeRoot.WriteFile(name, []byte("blocked"), 0o600); !errors.Is(err, file.ErrReadOnly) {
		t.Errorf("write file without write capability got %v, want read-only", err)
	}
	mode.Toggle(capWrite)
	if err := writeRoot.WriteFile(name, []byte("written"), 0o600); err != nil {
		t.Fatalf("write file with write capability: %v", err)
	}

	if _, _, err := files.Resolve(siblingPath); !errors.Is(err, file.ErrOutsideRoot) {
		t.Errorf("sibling resolved through file tools with %v", err)
	}
}

func TestAConfiguredFileSymlinkCannotDisguiseRepositoryMetadata(t *testing.T) {
	mode := NewMode(capRead | capWrite)
	files := configuredPathTestRoot(t, mode)
	repository := t.TempDir()
	target := filepath.Join(repository, ".git", "config")
	if err := os.Mkdir(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "shared")
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}

	roots, err := mountConfiguredPaths(files, mode, configuredPaths{Write: []string{alias}})
	if err != nil {
		t.Fatal(err)
	}
	defer closeConfiguredRoots(roots)

	mountedRoot, name, err := files.Resolve(alias)
	if err != nil {
		t.Fatal(err)
	}
	if err := mountedRoot.WriteFile(name, []byte("blocked"), 0o600); !errors.Is(err, file.ErrGitDir) {
		t.Errorf("repository metadata write got %v, want git refusal", err)
	}
	mode.Toggle(capGit)
	if err := mountedRoot.WriteFile(name, []byte("written"), 0o600); err != nil {
		t.Errorf("repository metadata write with git capability: %v", err)
	}
}
