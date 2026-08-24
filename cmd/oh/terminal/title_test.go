package terminal

import (
	"strings"
	"testing"

	"crdx.org/io/cmd/oh/caps"
)

func interactiveTitle(writer *strings.Builder) *title {
	return &title{writer: writer, isTerminal: true}
}

func TestTerminalBeginsUpdatesAndRestoresItsTitle(t *testing.T) {
	output := &strings.Builder{}
	managedTerminal := Terminal{title: interactiveTitle(output)}

	restore := managedTerminal.Begin(caps.Read | caps.Write)
	managedTerminal.SetMode(caps.Read)
	if got, want := output.String(), pushTitle+"\x1b]2;[w]\x1b\\\x1b]2;[r]\x1b\\"; got != want {
		t.Errorf("got title sequence %q, want %q", got, want)
	}

	output.Reset()
	restore()
	if got := output.String(); got != popTitle {
		t.Errorf("got restore sequence %q, want %q", got, popTitle)
	}
}

func TestTitleCannotContainTerminalControlCharacters(t *testing.T) {
	output := &strings.Builder{}
	managedTitle := interactiveTitle(output)

	managedTitle.Begin("oh\n\x1b]2;elsewhere\a")

	if got, want := output.String(), pushTitle+"\x1b]2;oh]2;elsewhere\x1b\\"; got != want {
		t.Errorf("got title sequence %q, want %q", got, want)
	}
}

func TestTerminalDoesNotWriteItsTitleToRedirectedOutput(t *testing.T) {
	output := &strings.Builder{}
	managedTerminal := New(output)

	restore := managedTerminal.Begin(caps.Read | caps.Write)
	managedTerminal.SetMode(caps.Read | caps.Write | caps.Git)
	restore()

	if output.Len() != 0 {
		t.Errorf("expected no title sequence, got %q", output.String())
	}
}
