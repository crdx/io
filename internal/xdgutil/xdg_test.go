package xdgutil_test

import (
	"path/filepath"
	"testing"

	"crdx.org/io/internal/xdgutil"
)

func TestStatePathUsesAnAbsoluteXDGStateHome(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", root)

	want := filepath.Join(root, "somewhere", "state")
	if got := xdgutil.StatePath("somewhere", "state"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStatePathIgnoresARelativeXDGStateHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join("inside", "workspace"))

	want := filepath.Join(home, ".local", "state", "somewhere")
	if got := xdgutil.StatePath("somewhere"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestConfigPathUsesAnAbsoluteXDGConfigHome(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)

	want := filepath.Join(root, "somewhere", "SYSTEM.md")
	if got := xdgutil.ConfigPath("somewhere", "SYSTEM.md"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestConfigPathIgnoresARelativeXDGConfigHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join("inside", "workspace"))

	want := filepath.Join(home, ".config", "somewhere")
	if got := xdgutil.ConfigPath("somewhere"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
