package pathutil_test

import (
	"path/filepath"
	"testing"

	"crdx.org/io/internal/pathutil"
)

func TestAbbrReturnsTheLastPathElement(t *testing.T) {
	path := filepath.Join("somewhere", "inside", "file.go")
	if got := pathutil.Abbr(path); got != "file.go" {
		t.Errorf("got %q, want file.go", got)
	}
}

func TestExistsReportsWhatCanBeStatted(t *testing.T) {
	directory := t.TempDir()
	if !pathutil.Exists(directory) {
		t.Errorf("expected %q to exist", directory)
	}
	if pathutil.Exists(filepath.Join(directory, "absent")) {
		t.Error("expected an absent path not to exist")
	}
}

func TestRelativeToNamesAPathInsideTheRoot(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "somewhere", "file.go")

	got, ok := pathutil.RelativeTo(root, path)
	if !ok {
		t.Fatal("expected the path to be inside the root")
	}
	if want := filepath.Join("somewhere", "file.go"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRelativeToRejectsAPathOutsideTheRoot(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(filepath.Dir(root), "outside")

	if name, ok := pathutil.RelativeTo(root, outside); ok {
		t.Errorf("expected an outside path to be rejected, got %q", name)
	}
}
