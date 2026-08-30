package menu

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"crdx.org/io/cmd/oh/key"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/oh/tty"
)

const (
	clearLine               = "\r\x1b[2K"
	hideCursor              = "\x1b[?25l"
	showCursor              = "\x1b[?25h"
	restoreMenuPresentation = "\x1b[0m" + showCursor
)

// ChooseIndex displays labels and returns the selected index.
func ChooseIndex(terminal *os.File, output io.Writer, prompt string, labels []string) (int, error) {
	if terminal == nil || !term.IsTerminal(int(terminal.Fd())) {
		return 0, errors.New("menu needs an interactive terminal")
	}

	restore, err := tty.Raw(terminal, output)
	if err != nil {
		return 0, err
	}
	defer restore()

	restorePresentation, err := beginMenuPresentation(output)
	if err != nil {
		return 0, err
	}
	defer restorePresentation()

	_, height, err := term.GetSize(int(terminal.Fd()))
	if err != nil {
		height = 24
	}

	menu := menu{
		labels: labels,
		rows:   min(max(height-4, 1), len(labels)),
	}
	if _, err := io.WriteString(output, RenderMenu(prompt, labels, 0)); err != nil {
		return 0, err
	}

	decoder := key.NewDecoder(bufio.NewReader(terminal))
	for {
		keypress, err := decoder.Next()
		if err != nil {
			return 0, err
		}

		switch keypress.Code {
		case key.Up:
			menu.move(-1)
		case key.Down:
			menu.move(1)
		case key.Home:
			menu.cursor = 0
			menu.scroll()
		case key.End:
			menu.cursor = len(labels) - 1
			menu.scroll()
		case key.Enter:
			return menu.cursor, nil
		case key.Escape:
			return 0, ErrCancelled
		case key.Rune:
			if keypress.Value == 'q' || keypress.Value == 'c' && keypress.Mod.Has(key.Ctrl) {
				return 0, ErrCancelled
			}
			continue
		case key.Backspace, key.Delete, key.Left, key.Right, key.PageUp, key.PageDown,
			key.PasteStart, key.PasteEnd, key.FocusIn, key.FocusOut, key.Unknown:
			continue
		default:
			continue
		}

		if _, err := fmt.Fprintf(output, "\x1b[%dA%s", menu.rows, menu.render(true)); err != nil {
			return 0, err
		}
	}
}

func beginMenuPresentation(output io.Writer) (func(), error) {
	if _, err := io.WriteString(output, hideCursor); err != nil {
		return nil, err
	}
	return func() { _, _ = io.WriteString(output, restoreMenuPresentation) }, nil
}

type menu struct {
	labels []string
	cursor int
	offset int
	rows   int
}

// RenderMenu renders a complete menu frame without terminal-control sequences.
func RenderMenu(prompt string, labels []string, cursor int) string {
	displayed := menu{labels: labels, cursor: cursor, rows: len(labels)}
	return fmt.Sprintf("%s\r\n\r\n%s", prompt, displayed.render(false))
}

func (self *menu) move(distance int) {
	self.cursor = min(max(self.cursor+distance, 0), len(self.labels)-1)
	self.scroll()
}

func (self *menu) scroll() {
	switch {
	case self.cursor < self.offset:
		self.offset = self.cursor
	case self.cursor >= self.offset+self.rows:
		self.offset = self.cursor - self.rows + 1
	}
}

func (self *menu) render(shouldClear bool) string {
	var rendered strings.Builder
	for i := self.offset; i < self.offset+self.rows; i++ {
		line := "  " + self.labels[i]
		if i == self.cursor {
			line = style.Chosen.Over("› " + self.labels[i])
		}
		if shouldClear {
			rendered.WriteString(clearLine)
		}
		rendered.WriteString(line)
		rendered.WriteString("\r\n")
	}
	return rendered.String()
}
