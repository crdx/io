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

func TestShortenWritesAPathTheWayTheUserWouldSayIt(t *testing.T) {
	t.Setenv("HOME", "/home/alice")

	tests := map[string]string{
		"/home/alice/proj/io":       "~/proj/io",
		"/home/alice":               "~",
		"/home/alice-other/project": "/home/alice-other/project", // a lookalike, not the home
		"/etc/hosts":                "/etc/hosts",
		"proj/io":                   "proj/io",
		"":                          "",
	}

	for path, want := range tests {
		if got := pathutil.Shorten(path); got != want {
			t.Errorf("got %q for %q, want %q", got, path, want)
		}
	}
}

func TestShortenLeavesAPathAloneWithoutAHome(t *testing.T) {
	t.Setenv("HOME", "")

	if got := pathutil.Shorten("/home/alice/proj"); got != "/home/alice/proj" {
		t.Errorf("got %q, want the path as it went in", got)
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
