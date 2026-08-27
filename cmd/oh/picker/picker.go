package picker

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"

	"crdx.org/io/cmd/oh/key"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/oh/tty"
	"crdx.org/io/cmd/oh/width"
	"crdx.org/io/internal/util/pathutil"
	"crdx.org/io/session"
)

const (
	enterScreen = "\x1b[?1049h\x1b[?25l"
	leaveScreen = "\x1b[?25h\x1b[?1049l"
	clearScreen = "\x1b[H\x1b[2J"
)

const (
	animalColumn      = 20
	columnGap         = 2
	minimumGap        = 2
	messageColumn     = 8
	lengthColumn      = 6
	lastMessageColumn = 12
	titlePrefixWidth  = 2
)

// ErrCancelled is the choice being abandoned, which is not a failure and is not a choice either.
var ErrCancelled = errors.New("cancelled")

// Session is the part of a stored session shown by the picker.
type Session struct {
	Name         string
	WorkspaceDir string
	Started      time.Time
	Touched      time.Time
	Title        string
	MessageCount int
	IsRunning    bool
}

// Messages counts what the user and the model said, excluding working events.
func (self *Session) Messages() int { return self.MessageCount }

// Choose shows the sessions of one workspace newest first and returns the chosen one. Abandoning
// the choice is ErrCancelled, so a session comes back whenever the error does not.
func Choose(sessions []*Session, workspaceDir string, terminal *os.File, screen io.Writer) (*Session, error) {
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
		sessions:     sessions,
		workspaceDir: workspaceDir,
		decoder:      key.NewDecoder(bufio.NewReader(terminal)),
		cursor:       firstSelectable(sessions),
		terminal:     terminal,
		screen:       screen,
	}).run()
}

type state struct {
	sessions     []*Session
	workspaceDir string
	decoder      *key.Decoder
	terminal     *os.File
	screen       io.Writer

	cursor int
	offset int
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
		self.cursor = firstSelectable(self.sessions)
	case key.End:
		self.cursor = lastSelectable(self.sessions)
	case key.Enter:
		if self.cursor >= 0 && !self.sessions[self.cursor].IsRunning {
			return sessionChosen
		}
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
	if self.cursor < 0 {
		return
	}

	for index := self.cursor + direction; index >= 0 && index < len(self.sessions); index += direction {
		if !self.sessions[index].IsRunning {
			self.cursor = index
			return
		}
	}
}

func firstSelectable(sessions []*Session) int {
	for i, storedSession := range sessions {
		if !storedSession.IsRunning {
			return i
		}
	}

	return -1
}

func lastSelectable(sessions []*Session) int {
	for i, storedSession := range slices.Backward(sessions) {
		if !storedSession.IsRunning {
			return i
		}
	}

	return -1
}

const keyHelp = "↑↓ to choose, enter to resume, q to cancel"

func header(workspaceDir string) string {
	return fmt.Sprintf("sessions in %s — %s", pathutil.Shorten(workspaceDir), keyHelp)
}

func (self *state) draw() {
	room, height := self.size()
	rows := max(height-3, 1)

	self.scroll(rows)

	var output strings.Builder

	output.WriteString(clearScreen)
	output.WriteString(style.Subtle(clip(header(self.workspaceDir), room)))
	output.WriteString("\r\n\r\n")
	output.WriteString(style.Subtle(columnHeader(room)))
	output.WriteString("\r\n")

	for i := self.offset; i < min(self.offset+rows, len(self.sessions)); i++ {
		output.WriteString(self.row(i, room))
		output.WriteString("\r\n")
	}

	write(self.screen, output.String())
}

func (self *state) scroll(rows int) {
	if self.cursor < 0 {
		return
	}

	switch {
	case self.cursor < self.offset:
		self.offset = self.cursor
	case self.cursor >= self.offset+rows:
		self.offset = self.cursor - rows + 1
	}
}

func (self *state) row(index int, room int) string {
	storedSession := self.sessions[index]
	line := row(storedSession, index == self.cursor, room)

	if storedSession.IsRunning {
		return style.Faint(line)
	}
	if index == self.cursor {
		return style.Chosen(line)
	}

	return style.Answer(line)
}

func columnHeader(room int) string {
	left := leftColumns(strings.Repeat(" ", titlePrefixWidth), "agent", "title")
	return columns(left, "messages", "length", "last message", room)
}

func row(storedSession *Session, isChosen bool, room int) string {
	prefix := mark(isChosen) + " "
	left := leftColumns(prefix, sessionAnimal(storedSession), sessionTitle(storedSession))
	return columns(
		left,
		strconv.Itoa(storedSession.Messages()),
		duration(storedSession.Touched.Sub(storedSession.Started)),
		ago(storedSession.Touched),
		room,
	)
}

func leftColumns(prefix string, animal string, title string) string {
	animal = clip(animal, animalColumn)
	return prefix + animal + strings.Repeat(" ", animalColumn-width.Of(animal)) + strings.Repeat(" ", columnGap) + title
}

func columns(left string, messages string, length string, lastMessage string, room int) string {
	right := fmt.Sprintf(
		"%*s  %*s  %*s",
		messageColumn,
		messages,
		lengthColumn,
		length,
		lastMessageColumn,
		lastMessage,
	)

	leftRoom := room - width.Of(right) - minimumGap
	if leftRoom <= 0 {
		return clip(left, room)
	}

	left = clip(left, leftRoom)
	return left + strings.Repeat(" ", room-width.Of(left)-width.Of(right)) + right
}

func duration(elapsed time.Duration) string {
	switch {
	case elapsed < time.Minute:
		return "<1m"
	case elapsed < time.Hour:
		return fmt.Sprintf("%dm", int(elapsed.Minutes()))
	case elapsed < 24*time.Hour:
		return fmt.Sprintf("%dh", int(elapsed.Hours()))
	}

	return fmt.Sprintf("%dd", int(elapsed.Hours()/24))
}

func mark(sessionChosen bool) string {
	if sessionChosen {
		return "›"
	}

	return " "
}

func sessionAnimal(storedSession *Session) string {
	emoji := session.Emoji(storedSession.Name)
	if emoji == "" {
		return storedSession.Name
	}

	return emoji + " " + storedSession.Name
}

func sessionTitle(storedSession *Session) string {
	if storedSession.Title == "" {
		return "(untitled)"
	}

	return strings.ReplaceAll(storedSession.Title, "\n", " ")
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
	room, height, err := term.GetSize(int(self.terminal.Fd()))
	if err != nil {
		return 80, 24
	}

	return room, height
}

func write(screen io.Writer, text string) {
	_, _ = io.WriteString(screen, text)
}
