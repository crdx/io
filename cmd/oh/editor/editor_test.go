package editor

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestConfiguredEditorOverridesVisual(t *testing.T) {
	t.Setenv("VISUAL", "visual-editor")

	command, err := buildCommand("configured-editor", "/tmp/config.toml")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(command.Args, []string{"configured-editor", "/tmp/config.toml"}) {
		t.Errorf("got arguments %v", command.Args)
	}
}

func TestVisualIsTheFallbackEditor(t *testing.T) {
	t.Setenv("VISUAL", "visual-editor")

	command, err := buildCommand("", "/tmp/config.toml")
	if err != nil {
		t.Fatal(err)
	}
	if command.Path != "visual-editor" && filepath.Base(command.Path) != "visual-editor" {
		t.Errorf("got executable %q", command.Path)
	}
}

func TestAnEditorMustBeAvailable(t *testing.T) {
	t.Setenv("VISUAL", "")

	_, err := buildCommand(" ", "/tmp/config.toml")
	if err == nil || !strings.Contains(err.Error(), "VISUAL") {
		t.Errorf("got %v", err)
	}
}

func TestOpenReportsAnEditorThatCannotStart(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	err := Open("missing-editor", "/tmp/config.toml")
	if err == nil || !strings.Contains(err.Error(), "could not start editor") {
		t.Errorf("got %v", err)
	}
}
