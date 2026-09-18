package util

import "testing"

func TestNameGlobsMatchEveryPathSegment(t *testing.T) {
	for _, name := range []string{
		"foo.txt",
		"/workspace/foo.txt",
		"/workspace/foo.txt/inside",
	} {
		if !MatchNameGlobs([]string{"foo.*"}, name) {
			t.Errorf("%q did not match", name)
		}
	}
	if MatchNameGlobs([]string{"foo.*"}, "/workspace/food.txt") {
		t.Error("a different name matched")
	}
}

func TestNameGlobsRejectPathsAndMalformedPatterns(t *testing.T) {
	for _, pattern := range []string{"", "directory/name", `directory\\name`, "broken["} {
		if err := ValidateNameGlob(pattern); err == nil {
			t.Errorf("%q was accepted", pattern)
		}
	}
}
