package util_test

import (
	"testing"

	"crdx.org/io/internal/util"
)

func TestASearchIsDescribedByItsPatternAndWhatQualifiesIt(t *testing.T) {
	pattern, qualifier := util.DescribeSearch("func New", "internal", "*.go")

	if pattern != "func New" {
		t.Errorf("got pattern %q, want the pattern searched for", pattern)
	}
	if want := "internal *.go"; qualifier != want {
		t.Errorf("got qualifier %q, want %q", qualifier, want)
	}
}

func TestASearchBelowHomeIsQualifiedWithATilde(t *testing.T) {
	t.Setenv("HOME", "/home/alice")

	if _, qualifier := util.DescribeSearch("func New", "/home/alice/proj/io", ""); qualifier != "~/proj/io" {
		t.Errorf("got qualifier %q, want the path written with a tilde", qualifier)
	}
}

func TestTheWorkingDirectoryGoesWithoutSaying(t *testing.T) {
	if _, qualifier := util.DescribeSearch("func New", ".", ""); qualifier != "" {
		t.Errorf("got qualifier %q, want nothing said about the working directory", qualifier)
	}
}
