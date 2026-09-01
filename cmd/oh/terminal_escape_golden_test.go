package main

import (
	"bufio"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/edit"
	"crdx.org/io/cmd/oh/key"
	"crdx.org/io/cmd/oh/output"
)

const terminalInputColumns = 40

func TestTerminalEscapeDrawsWhatItDrewBefore(t *testing.T) {
	passes := map[string]func() string{
		"bare escape keeps the following key": func() string {
			return drawBareEscapeFollowedByKey(t)
		},
		"fragmented arrow remains one key": func() string {
			return drawFragmentedArrow(t)
		},
	}

	screenPasses := map[string]func() string{}
	for name, pass := range passes {
		screenPasses[name] = func() string {
			return shown(t, pass(), terminalInputColumns)
		}
	}

	compareWithGolden(t, "terminal-escape", ".ansi", passes)
	compareWithGolden(t, "terminal-escape", ".screen", screenPasses)
}

func drawBareEscapeFollowedByKey(t *testing.T) string {
	t.Helper()

	self, editor, history, screenOutput := terminalInputFixture(t, "draft", nil)
	terminalInput := newTerminalInput(t)
	terminalInput.apply(t, self, editor, history, "\x1b")
	terminalInput.apply(t, self, editor, history, "X")

	if editor.Text() != "draftX" {
		t.Fatalf("draft after Escape and X = %q", editor.Text())
	}

	return screenOutput.String()
}

func drawFragmentedArrow(t *testing.T) string {
	t.Helper()

	self, editor, history, screenOutput := terminalInputFixture(t, "draft", []string{"earlier"})
	terminalInput := newTerminalInput(t)
	terminalInput.apply(t, self, editor, history, "\x1b[A")

	if editor.Text() != "earlier" {
		t.Fatalf("draft after fragmented Up = %q", editor.Text())
	}

	return screenOutput.String()
}

func terminalInputFixture(t *testing.T, text string, historyLines []string) (*App, *edit.Input, *edit.History, *strings.Builder) {
	t.Helper()

	self := slashCommandFixture(t, caps.Read)
	var screenOutput strings.Builder
	self.screen = output.NewTerminalOfSize(&screenOutput, terminalInputColumns, replayLines)

	history := edit.NewHistory("", historyLimit)
	for _, line := range historyLines {
		history.Add(line)
	}
	editor := edit.NewInput(history)
	editor.SetText(text)
	self.show(editor)

	return self, editor, history, &screenOutput
}

type oneByteReader struct {
	reader io.Reader
}

func (self oneByteReader) Read(buffer []byte) (int, error) {
	return self.reader.Read(buffer[:min(len(buffer), 1)])
}

type terminalInput struct {
	decoder *key.Decoder
	writer  *os.File
}

type decodedTerminalKey struct {
	keypress key.Key
	err      error
}

func newTerminalInput(t *testing.T) terminalInput {
	t.Helper()

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close() })
	t.Cleanup(func() { _ = writer.Close() })

	fragmentedReader := oneByteReader{reader: reader}
	return terminalInput{
		decoder: key.NewTerminalDecoder(bufio.NewReader(fragmentedReader), reader),
		writer:  writer,
	}
}

func (self terminalInput) apply(t *testing.T, app *App, editor *edit.Input, history *edit.History, input string) {
	t.Helper()

	if _, err := self.writer.WriteString(input); err != nil {
		t.Fatal(err)
	}
	decoded := make(chan decodedTerminalKey, 1)
	go func() {
		keypress, err := self.decoder.Next()
		decoded <- decodedTerminalKey{keypress: keypress, err: err}
	}()

	select {
	case result := <-decoded:
		if result.err != nil {
			t.Fatal(result.err)
		}
		if !app.handleKeypressAndShowInput(editor, history, result.keypress) {
			t.Fatal("terminal input unexpectedly closed the conversation")
		}
	case <-time.After(time.Second):
		t.Fatal("terminal input was not decoded")
	}
}
