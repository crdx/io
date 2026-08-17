package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestARelativeXDGStateHomeIsIgnored(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", filepath.Join("inside", "workspace"))

	got := sessionsDir()
	if !filepath.IsAbs(got) {
		t.Fatalf("got relative state path %q", got)
	}
	if strings.HasPrefix(got, filepath.Join("inside", "workspace")) {
		t.Errorf("used the relative XDG_STATE_HOME in %q", got)
	}
}

func TestAnAbsoluteXDGStateHomeIsUsed(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", root)

	want := filepath.Join(root, namespace, app, "sessions")
	if got := sessionsDir(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
