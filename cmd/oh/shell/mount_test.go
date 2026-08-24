package shell

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/internal/file"
)

func configuredPathTestRoot(t *testing.T, mode *caps.Mode) *file.Root {
	t.Helper()

	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return file.New(root, caps.RefuseWrite(mode))
}

func TestMissingConfiguredPathsAreCreatedAndKept(t *testing.T) {
	existingRead := filepath.Join(t.TempDir(), "read")
	if err := os.WriteFile(existingRead, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	existingWrite := t.TempDir()
	existingExec := filepath.Join(t.TempDir(), "exec")
	if err := os.WriteFile(existingExec, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	existingHome := filepath.Join(t.TempDir(), "home")
	if err := os.WriteFile(existingHome, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	missingRead := filepath.Join(t.TempDir(), "missing-read")
	missingWrite := filepath.Join(t.TempDir(), "missing-write")
	missingExec := filepath.Join(t.TempDir(), "missing-exec")
	missingHome := filepath.Join(t.TempDir(), "missing-home")

	var warnings strings.Builder
	filtered, err := PreparePaths(Paths{
		Read:  []string{existingRead, missingRead},
		Write: []string{existingWrite, missingWrite},
		Exec:  []string{existingExec, missingExec},
		Home:  []string{existingHome, missingHome},
	}, &warnings)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(filtered.Read, []string{existingRead, missingRead}) {
		t.Errorf("got read paths %v, want %v", filtered.Read, []string{existingRead, missingRead})
	}
	if !slices.Equal(filtered.Write, []string{existingWrite, missingWrite}) {
		t.Errorf("got write paths %v, want %v", filtered.Write, []string{existingWrite, missingWrite})
	}
	if !slices.Equal(filtered.Exec, []string{existingExec, missingExec}) {
		t.Errorf("got executable paths %v, want %v", filtered.Exec, []string{existingExec, missingExec})
	}
	if !slices.Equal(filtered.Home, []string{existingHome, missingHome}) {
		t.Errorf("got mapped paths %v, want %v", filtered.Home, []string{existingHome, missingHome})
	}
	for _, path := range []string{missingRead, missingWrite, missingExec, missingHome} {
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("missing path %s was not created: %v", path, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("missing path %s was not created as a directory", path)
		}
	}
	if warnings.String() != "" {
		t.Errorf("got warnings %q, want none", warnings.String())
	}
}

func TestUncreatableConfiguredPathsAreWarnedAboutAndSkipped(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}

	parent := filepath.Join(t.TempDir(), "read-only")
	if err := os.Mkdir(parent, 0o500); err != nil {
		t.Fatal(err)
	}
	uncreatable := filepath.Join(parent, "child")

	var warnings strings.Builder
	filtered, err := PreparePaths(Paths{
		Read: []string{uncreatable},
	}, &warnings)
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered.Read) != 0 {
		t.Errorf("got read paths %v, want none", filtered.Read)
	}
	if !strings.Contains(warnings.String(), "warning: could not create configured path "+uncreatable) {
		t.Errorf("warning does not name uncreatable path %s: %q", uncreatable, warnings.String())
	}
}

func TestConfiguredPathsAreMountedWithTheirRequestedFileAccess(t *testing.T) {
	workspace := t.TempDir()
	workspaceRoot, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workspaceRoot.Close() }()

	mode := caps.NewMode(caps.Read)
	files := file.New(workspaceRoot, caps.RefuseWrite(mode))
	readDirectory := t.TempDir()
	writeDirectory := t.TempDir()
	execDirectory := t.TempDir()
	if err := os.Mkdir(filepath.Join(writeDirectory, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(readDirectory, "reference"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	roots, err := MountPaths(files, mode, Paths{
		Read:  []string{readDirectory},
		Write: []string{writeDirectory},
		Exec:  []string{execDirectory},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer CloseRoots(roots)

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
	mode.Toggle(caps.Write)
	if err := writeRoot.WriteFile(name, []byte("written"), 0o600); err != nil {
		t.Fatalf("write path with write capability: %v", err)
	}
	if err := writeRoot.WriteFile(filepath.Join(".git", "config"), nil, 0o600); !errors.Is(err, file.ErrGitDir) {
		t.Errorf("repository metadata write got %v, want git refusal", err)
	}
	mode.Toggle(caps.Git)
	if err := writeRoot.WriteFile(filepath.Join(".git", "config"), nil, 0o600); err != nil {
		t.Errorf("repository metadata write with git capability: %v", err)
	}

	if _, _, err := files.Resolve(execDirectory); !errors.Is(err, file.ErrOutsideRoot) {
		t.Errorf("exec-only path resolved through file tools with %v", err)
	}
}

func TestConfiguredFilesAreMountedWithoutTheirSiblings(t *testing.T) {
	mode := caps.NewMode(caps.Read)
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

	roots, err := MountPaths(files, mode, Paths{
		Read:  []string{readPath, writePath},
		Write: []string{writePath},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer CloseRoots(roots)

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
	mode.Toggle(caps.Write)
	if err := writeRoot.WriteFile(name, []byte("written"), 0o600); err != nil {
		t.Fatalf("write file with write capability: %v", err)
	}

	if _, _, err := files.Resolve(siblingPath); !errors.Is(err, file.ErrOutsideRoot) {
		t.Errorf("sibling resolved through file tools with %v", err)
	}
}

func TestAConfiguredFileSymlinkCannotDisguiseRepositoryMetadata(t *testing.T) {
	mode := caps.NewMode(caps.Read | caps.Write)
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

	roots, err := MountPaths(files, mode, Paths{Write: []string{alias}})
	if err != nil {
		t.Fatal(err)
	}
	defer CloseRoots(roots)

	mountedRoot, name, err := files.Resolve(alias)
	if err != nil {
		t.Fatal(err)
	}
	if err := mountedRoot.WriteFile(name, []byte("blocked"), 0o600); !errors.Is(err, file.ErrGitDir) {
		t.Errorf("repository metadata write got %v, want git refusal", err)
	}
	mode.Toggle(caps.Git)
	if err := mountedRoot.WriteFile(name, []byte("written"), 0o600); err != nil {
		t.Errorf("repository metadata write with git capability: %v", err)
	}
}
