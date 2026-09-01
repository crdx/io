package main

import (
	"bytes"
	"strings"
	"testing"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/edit"
	"crdx.org/io/cmd/oh/output"
)

func TestLegacyAltEnterDrawsWhatItDrewBefore(t *testing.T) {
	compareWithGolden(t, "legacy-alt-enter", ".ansi", map[string]func() string{
		"force sends an unknown command": func() string {
			return drawLegacyAltEnter(t)
		},
	})
	compareWithGolden(t, "legacy-alt-enter", ".screen", map[string]func() string{
		"force sends an unknown command": func() string {
			return shown(t, drawLegacyAltEnter(t), terminalInputColumns)
		},
	})
}

func drawLegacyAltEnter(t *testing.T) string {
	t.Helper()

	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	self.screen = output.NewTerminalOfSize(&screenOutput, terminalInputColumns, replayLines)
	history := edit.NewHistory("", historyLimit)
	editor := edit.NewInput(history)
	editor.SetText("/nosuch")
	self.show(editor)

	terminalInput := newTerminalInput(t)
	terminalInput.apply(t, self, editor, history, "\x1b\r")

	for report := range self.currentTurn.Events() {
		self.takeTurn(report)
	}
	self.finish()
	self.show(editor)
	self.screen.End()

	if !hasUserMessage(self.events, "/nosuch") {
		t.Fatal("legacy Alt+Enter did not send the unknown command")
	}
	if strings.Contains(screenOutput.String(), "Command not found") {
		t.Fatalf("legacy Alt+Enter rejected the command: %q", screenOutput.String())
	}

	return screenOutput.String()
}

func hasUserMessage(events []agent.Event, text string) bool {
	for _, event := range events {
		if event.Kind == agent.UserMessageEvent && event.Text == text {
			return true
		}
	}

	return false
}
