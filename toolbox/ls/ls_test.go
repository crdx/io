package ls_test

import (
	"os"
	"path/filepath"
	"testing"

	"crdx.org/io/internal/file"
	"crdx.org/io/toolbox/ls"
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

	call, err := ls.New(root).Parse(arguments)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return call.Exec(t.Context())
}

func TestADirectoryIsMarkedWithASlash(t *testing.T) {
	root, directory := testRoot(t)

	if err := os.Mkdir(filepath.Join(directory, "inner"), 0o750); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := os.WriteFile(filepath.Join(directory, "a.txt"), nil, 0o600); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output, err := exec(t, root, `{}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output != "a.txt\ninner/" {
		t.Errorf("expected the entries sorted with the directory marked, got %q", output)
	}
}

func TestAnEmptyDirectorySaysSo(t *testing.T) {
	root, _ := testRoot(t)

	output, err := exec(t, root, `{"path":"."}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output != "(empty)" {
		t.Errorf("expected an empty listing to say so, got %q", output)
	}
}

func TestListingSomethingThatIsNotThereIsRefused(t *testing.T) {
	root, _ := testRoot(t)

	if _, err := exec(t, root, `{"path":"nowhere"}`); err == nil {
		t.Error("expected a missing directory to be refused")
	}
}

func TestRenderSaysNothingOfTheWorkingDirectory(t *testing.T) {
	if renderedPath, _ := ls.Render(ls.Args{}); renderedPath != "" {
		t.Errorf("expected nothing, got %q", renderedPath)
	}

	if renderedPath, _ := ls.Render(ls.Args{Path: "."}); renderedPath != "" {
		t.Errorf("expected nothing, got %q", renderedPath)
	}
}

func allowAll(string) error { return nil }
