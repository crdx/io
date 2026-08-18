package util_test

import (
	"testing"

	"crdx.org/io/internal/util"
)

func TestASearchIsDescribedByItsPatternAndWhatQualifiesIt(t *testing.T) {
	pattern, detail := util.RenderSearch("func New", "internal", "*.go")

	if pattern != "func New" {
		t.Errorf("got pattern %q, want the pattern searched for", pattern)
	}
	if want := "in internal (*.go)"; detail != want {
		t.Errorf("got detail %q, want %q", detail, want)
	}
}

func TestASearchBelowHomeIsQualifiedWithATilde(t *testing.T) {
	t.Setenv("HOME", "/home/alice")

	if _, detail := util.RenderSearch("func New", "/home/alice/proj/io", ""); detail != "in ~/proj/io" {
		t.Errorf("got detail %q, want the path written with a tilde", detail)
	}
}

func TestTheWorkingDirectoryGoesWithoutSaying(t *testing.T) {
	if _, detail := util.RenderSearch("func New", ".", ""); detail != "" {
		t.Errorf("got detail %q, want nothing said about the working directory", detail)
	}
}
