// Package picker chooses one stored session from a list, on the alternate screen so the
// conversation that follows starts on a clean scrollback.
package picker

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"golang.org/x/term"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/key"
	"crdx.org/io/cmd/oh/theme"
	"crdx.org/io/cmd/oh/tty"
	"crdx.org/io/cmd/oh/width"
	"crdx.org/io/internal/pathutil"
)

const (
	enterScreen = "\x1b[?1049h\x1b[?25l"
	leaveScreen = "\x1b[?25h\x1b[?1049l"
	clearScreen = "\x1b[H\x1b[2J"
)

// ErrCancelled is the choice being abandoned, which is not a failure and is not a choice either.
var ErrCancelled = errors.New("cancelled")

// Session is the part of a stored session shown by the picker.
type Session struct {
	ID           string
	WorkspaceDir string
	Touched      time.Time
	Events       []agent.Event
}

// FirstPrompt is the first prompt in the session.
func (self *Session) FirstPrompt() string {
	for _, event := range self.Events {
		if event.Kind == agent.Prompt {
			return event.Text
		}
	}
	return ""
}

// Messages counts prompts and answers, excluding working events.
func (self *Session) Messages() int {
	count := 0
	for _, event := range self.Events {
		if event.Kind == agent.Prompt || event.Kind == agent.Text {
			count++
		}
	}
	return count
}

// Choose shows the sessions newest first and returns the chosen one. Abandoning the choice is
// ErrCancelled, so a session comes back whenever the error does not.
func Choose(sessions []*Session, terminal *os.File, screen io.Writer) (*Session, error) {
	if len(sessions) == 0 {
		return nil, ErrCancelled
	}

	restore, err := tty.Raw(terminal, screen)
	if err != nil {
		return nil, err
	}

	defer restore()

	write(screen, enterScreen)
	defer write(screen, leaveScreen)

	return (&state{
		sessions: sessions,
		decoder:  key.NewDecoder(bufio.NewReader(terminal)),
		terminal: terminal,
		screen:   screen,
	}).run()
}

type state struct {
	sessions []*Session   // the sessions on offer
	decoder  *key.Decoder // the keyboard input
	terminal *os.File     // the terminal being controlled
	screen   io.Writer    // where the list is drawn

	cursor int // the selected row
	offset int // the first visible row
}

func (self *state) run() (*Session, error) {
	for {
		self.draw()

		keypress, err := self.decoder.Next()
		if err != nil {
			return nil, err
		}

		switch self.apply(keypress) {
		case sessionChosen:
			return self.sessions[self.cursor], nil
		case choiceCancelled:
			return nil, ErrCancelled
		case continuePicking:
		}
	}
}

type action int

const (
	continuePicking action = iota
	sessionChosen
	choiceCancelled
)

func (self *state) apply(keypress key.Key) action {
	switch keypress.Code {
	case key.Up:
		self.move(-1)
	case key.Down:
		self.move(1)
	case key.Home:
		self.cursor = 0
	case key.End:
		self.cursor = len(self.sessions) - 1
	case key.Enter:
		return sessionChosen
	case key.Escape:
		return choiceCancelled
	case key.Rune:
		if keypress.Value == 'q' || (keypress.Value == 'c' && keypress.Mod.Has(key.Ctrl)) {
			return choiceCancelled
		}
	}

	return continuePicking
}

func (self *state) move(direction int) {
	self.cursor = min(max(self.cursor+direction, 0), len(self.sessions)-1)
}

const header = "sessions — ↑↓ to choose, enter to resume, q to cancel"

func (self *state) draw() {
	width, height := self.size()
	rows := max(height-2, 1)

	self.scroll(rows)

	var out strings.Builder

	out.WriteString(clearScreen)
	out.WriteString(theme.Subtle(clip(header, width)))
	out.WriteString("\r\n\r\n")

	for index := self.offset; index < min(self.offset+rows, len(self.sessions)); index++ {
		out.WriteString(self.row(index, width))
		out.WriteString("\r\n")
	}

	write(self.screen, out.String())
}

func (self *state) scroll(rows int) {
	switch {
	case self.cursor < self.offset:
		self.offset = self.cursor
	case self.cursor >= self.offset+rows:
		self.offset = self.cursor - rows + 1
	}
}

func (self *state) row(index int, width int) string {
	storedSession := self.sessions[index]

	line := fmt.Sprintf(
		"%s  %-12s  %7s  %s  %s",
		mark(index == self.cursor),
		ago(storedSession.Touched),
		messageCount(storedSession.Messages()),
		pathutil.Shorten(storedSession.WorkspaceDir),
		title(storedSession),
	)

	line = clip(line, width)

	if index == self.cursor {
		return theme.Chosen(line)
	}

	return theme.Answer(line)
}

func messageCount(messages int) string {
	return fmt.Sprintf("%d msg", messages) // not %dm, which the column beside it already means
}

func mark(sessionChosen bool) string {
	if sessionChosen {
		return "›"
	}

	return " "
}

func title(storedSession *Session) string {
	askedText := storedSession.FirstPrompt()
	if askedText == "" {
		return "(nothing was asked)"
	}

	return strings.ReplaceAll(askedText, "\n", " ")
}

func ago(when time.Time) string {
	elapsed := time.Since(when)

	switch {
	case elapsed < time.Minute:
		return "just now"
	case elapsed < time.Hour:
		return fmt.Sprintf("%dm ago", int(elapsed.Minutes()))
	case elapsed < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(elapsed.Hours()))
	}

	return fmt.Sprintf("%dd ago", int(elapsed.Hours()/24))
}

func clip(text string, room int) string {
	if room <= 0 {
		return ""
	}

	if width.Of(text) <= room {
		return text
	}

	prefix, _ := width.Cut(text, room-1)

	return prefix + "…"
}

func (self *state) size() (int, int) {
	width, height, err := term.GetSize(int(self.terminal.Fd()))
	if err != nil {
		return 80, 24
	}

	return width, height
}

func write(screen io.Writer, text string) {
	_, _ = io.WriteString(screen, text)
}
