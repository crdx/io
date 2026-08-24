package shell

import (
	"path/filepath"
	"testing"
)

func TestAnExplicitMiseDataDirectoryIsPreserved(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MISE_DATA_DIR", dataDir)
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	if got := miseDataDir(); got != dataDir {
		t.Errorf("got %q, want %q", got, dataDir)
	}
}

func TestMiseDataUsesTheOriginalXDGDataHome(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("MISE_DATA_DIR", "")
	t.Setenv("XDG_DATA_HOME", dataHome)

	want := filepath.Join(dataHome, "mise")
	if got := miseDataDir(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMiseDataFallsBackToTheOriginalHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MISE_DATA_DIR", "")
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("HOME", home)

	want := filepath.Join(home, ".local", "share", "mise")
	if got := miseDataDir(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
