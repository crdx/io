package picker

import (
	"errors"
	"io"
	"os"
	"os/signal"
	"slices"
	"strings"
	"unicode/utf8"

	"golang.org/x/term"

	"crdx.org/io/cmd/oh/interaction"
	"crdx.org/io/cmd/oh/key"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/oh/tty"
	"crdx.org/io/cmd/oh/width"
)

const (
	enterScreen = "\x1b[?1049h\x1b[?25l"
	leaveScreen = "\x1b[?25h\x1b[?1049l"
	homeCursor  = "\x1b[H"
	eraseLine   = "\x1b[K"
	eraseBelow  = "\x1b[J"
)

const (
	ColumnGap = 2

	RoomForModel      = 100
	RoomForIdentifier = 140

	IdentifierColumn = 24
	EffortColumn     = 8
)

const (
	minimumGap     = 2
	defaultColumns = 80
	defaultRows    = 24
)

const filterPrompt = "Filter: "

var ErrCancelled = errors.New("cancelled")

type List interface {
	Len() int
	IsChoosable(index int) bool
	Text(index int) string
	ColumnHeader(room int) string
	Row(index int, isChosen bool, room int) string
	Adjust(index int, direction int)
}

func Choose(rows List, terminal *os.File, screen io.Writer) (int, error) {
	if rows.Len() == 0 {
		return 0, ErrCancelled
	}

	restore, err := tty.Raw(terminal, screen)
	if err != nil {
		return 0, err
	}

	defer restore()

	write(screen, enterScreen)
	defer write(screen, leaveScreen)

	self := newState(rows)
	self.keys = interaction.Keypresses(terminal)
	self.measure = measuring(terminal)
	self.screen = screen

	return self.run()
}

func newState(rows List) *state {
	self := &state{list: rows}
	self.refilter()

	return self
}

func measuring(terminal *os.File) func() (int, int) {
	return func() (int, int) {
		room, height, err := term.GetSize(int(terminal.Fd()))
		if err != nil {
			return defaultColumns, defaultRows
		}

		return room, height
	}
}

type state struct {
	list    List
	keys    <-chan key.Key
	measure func() (int, int)
	screen  io.Writer

	query   string
	matches []int
	cursor  int
	offset  int
	window  int
}

func (self *state) run() (int, error) {
	resizeSignals := interaction.Resizes()
	defer signal.Stop(resizeSignals)

	return self.pick(resizeSignals)
}

func (self *state) pick(resizeSignals <-chan os.Signal) (int, error) {
	for {
		self.draw()

		select {
		case keypress, open := <-self.keys:
			if !open {
				return 0, ErrCancelled
			}

			switch self.apply(keypress) {
			case rowChosen:
				return self.chosen(), nil
			case choiceCancelled:
				return 0, ErrCancelled
			case continuePicking:
			}
		case <-resizeSignals:
			interaction.Settle(resizeSignals)
		}
	}
}

type action int

const (
	continuePicking action = iota
	rowChosen
	choiceCancelled
)

func (self *state) apply(keypress key.Key) action {
	switch keypress.Code {
	case key.Up:
		self.move(-1)
	case key.Down:
		self.move(1)
	case key.Left:
		self.adjust(-1)
	case key.Right:
		self.adjust(1)
	case key.PageUp:
		self.page(-1)
	case key.PageDown:
		self.page(1)
	case key.Home:
		self.cursor = self.firstSelectable()
	case key.End:
		self.cursor = self.lastSelectable()
	case key.Enter:
		if self.cursor >= 0 && self.list.IsChoosable(self.chosen()) {
			return rowChosen
		}
	case key.Escape:
		return choiceCancelled
	case key.Backspace:
		self.narrow(trimLastRune(self.query))
	case key.Rune:
		if keypress.Value == 'c' && keypress.Mod.Has(key.Ctrl) {
			return choiceCancelled
		}
		if isTypeable(keypress) {
			self.narrow(self.query + string(keypress.Value))
		}
	}

	return continuePicking
}

func isTypeable(keypress key.Key) bool {
	return keypress.Value >= ' ' && keypress.Value != 0x7f && !keypress.Mod.Has(key.Ctrl) &&
		!keypress.Mod.Has(key.Alt)
}

func trimLastRune(text string) string {
	if text == "" {
		return ""
	}

	_, size := utf8.DecodeLastRuneInString(text)

	return text[:len(text)-size]
}

func (self *state) chosen() int {
	return self.matches[self.cursor]
}

func (self *state) narrow(query string) {
	if query == self.query {
		return
	}

	wanted := -1
	if self.cursor >= 0 {
		wanted = self.chosen()
	}

	self.query = query
	self.refilter()

	if at := slices.Index(self.matches, wanted); at >= 0 && self.list.IsChoosable(wanted) {
		self.cursor = at
	}
}

func (self *state) refilter() {
	self.matches = self.matches[:0]

	for index := range self.list.Len() {
		if matchesQuery(self.list.Text(index), self.query) {
			self.matches = append(self.matches, index)
		}
	}

	self.cursor = self.firstSelectable()
	self.offset = 0
}

func matchesQuery(text string, query string) bool {
	text = strings.ToLower(text)

	for word := range strings.FieldsSeq(strings.ToLower(query)) {
		if !strings.Contains(text, word) {
			return false
		}
	}

	return true
}

func (self *state) move(direction int) {
	if self.cursor < 0 {
		return
	}

	for at := self.cursor + direction; at >= 0 && at < len(self.matches); at += direction {
		if self.list.IsChoosable(self.matches[at]) {
			self.cursor = at
			return
		}
	}
}

func (self *state) page(direction int) {
	for range max(self.window, 1) {
		self.move(direction)
	}
}

func (self *state) adjust(direction int) {
	if self.cursor >= 0 {
		self.list.Adjust(self.chosen(), direction)
	}
}

func (self *state) firstSelectable() int {
	for at, index := range self.matches {
		if self.list.IsChoosable(index) {
			return at
		}
	}

	return -1
}

func (self *state) lastSelectable() int {
	for at, index := range slices.Backward(self.matches) {
		if self.list.IsChoosable(index) {
			return at
		}
	}

	return -1
}

func Paint(rows List, room int, height int, cursor int, query string) string {
	var screen strings.Builder

	self := newState(rows)
	self.narrow(query)
	self.cursor = cursor
	self.screen = &screen
	self.measure = func() (int, int) { return room, height }

	self.draw()

	return screen.String()
}

func (self *state) draw() {
	room, height := self.size()
	rows := max(height-2, 1)
	self.window = rows

	self.scroll(rows)

	var output strings.Builder

	output.WriteString(homeCursor)
	output.WriteString(self.filterLine(room))
	output.WriteString(eraseLine + "\r\n")
	output.WriteString(style.Column(Clip(self.list.ColumnHeader(room), room)))
	output.WriteString(eraseLine + "\r\n")

	for at := self.offset; at < self.offset+rows; at++ {
		if at < len(self.matches) {
			output.WriteString(self.list.Row(self.matches[at], at == self.cursor, room))
		}
		output.WriteString(eraseLine)
		if at+1 < self.offset+rows {
			output.WriteString("\r\n")
		}
	}

	output.WriteString(eraseBelow)

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

func (self *state) filterLine(room int) string {
	return style.Subtle(Clip(filterPrompt, room)) + style.Answer(Clip(self.query, room-len(filterPrompt)))
}

func Columns(left string, right string, room int) string {
	leftRoom := room - width.Of(right) - minimumGap
	if leftRoom <= 0 {
		return Clip(left, room)
	}

	left = Clip(left, leftRoom)

	return strings.TrimRight(left+strings.Repeat(" ", room-width.Of(left)-width.Of(right))+right, " ")
}

func Mark(isChosen bool) string {
	if isChosen {
		return "›"
	}

	return " "
}

func Pad(text string, room int) string {
	text = Clip(text, room)
	return text + strings.Repeat(" ", room-width.Of(text))
}

func Clip(text string, room int) string {
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
	if self.measure == nil {
		return defaultColumns, defaultRows
	}

	return self.measure()
}

func write(screen io.Writer, text string) {
	_, _ = io.WriteString(screen, text)
}
