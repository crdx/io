package editor

import (
	"bytes"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

func TestConfiguredEditorIsUsed(t *testing.T) {
	command, err := buildCommand(Command{"configured-editor", "--wait"}, []string{"/tmp/config.toml"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(command.Args, []string{"configured-editor", "--wait", "/tmp/config.toml"}) {
		t.Errorf("got arguments %v", command.Args)
	}
}

func TestEveryPathIsPassedToTheEditor(t *testing.T) {
	command, err := buildCommand(Command{"configured-editor"}, []string{"/tmp/skills", "/tmp/more-skills"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(command.Args, []string{"configured-editor", "/tmp/skills", "/tmp/more-skills"}) {
		t.Errorf("got arguments %v", command.Args)
	}
}

func TestAnEditorMustBeConfigured(t *testing.T) {
	_, err := buildCommand(Command{" "}, []string{"/tmp/config.toml"})
	if err == nil || !strings.Contains(err.Error(), "set editor in config.toml") {
		t.Errorf("got %v", err)
	}
}

func TestOpenReportsAnEditorThatCannotStart(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	err := Open(Command{"missing-editor"}, "/tmp/config.toml")
	if err == nil || !strings.Contains(err.Error(), "could not start editor") {
		t.Errorf("got %v", err)
	}
}

func TestEditorExitFailureIsReported(t *testing.T) {
	command := exec.Command("sh", "-c", "exit 42")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}

	var errors bytes.Buffer
	reportExit(command, &errors)
	if !strings.Contains(errors.String(), "exit status 42") {
		t.Errorf("got error output %q", errors.String())
	}
}
