package menu

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
	"crdx.org/io/internal/util/strutil"
)

const (
	enterScreen = "\x1b[?1049h" + hideCursor
	leaveScreen = showCursor + "\x1b[?1049l"
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

const (
	filterPrompt   = "Filter: "
	removalAnswers = "(y/n)"
)

var ErrCancelled = errors.New("cancelled")

type List interface {
	Len() int
	IsChoosable(index int) bool
	Text(index int) string
	ColumnHeader(room int) string
	Row(index int, isChosen bool, room int) string
	Adjust(index int, direction int)
}

type Removable interface {
	IsRemovable(index int) bool
	RemovalPrompt(index int) string
	Remove(index int) error
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

	keys, stopReading := interaction.Keypresses(terminal)
	defer stopReading()

	return choose(rows, keys, measuring(terminal), screen)
}

func choose(rows List, keys <-chan key.Key, measure func() (int, int), screen io.Writer) (int, error) {
	write(screen, enterScreen)
	defer write(screen, leaveScreen)

	self := newState(rows)
	self.keys = keys
	self.measure = measure
	self.screen = screen

	return self.run()
}

func newState(rows List) *state {
	self := &state{list: rows, removal: removal{index: -1}}
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

	removal removal
}

type removal struct {
	index   int
	failure string
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
		case keypress, isOpen := <-self.keys:
			if !isOpen {
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
	if self.removal.index >= 0 {
		self.answerRemoval(keypress)
		return continuePicking
	}

	self.removal.failure = ""

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
	case key.Delete:
		self.askToRemove()
	case key.PasteStart, key.PasteEnd, key.FocusIn, key.FocusOut, key.Unknown:
	}

	return continuePicking
}

func (self *state) removableList() (Removable, bool) {
	rows, isRemovable := self.list.(Removable)
	return rows, isRemovable
}

func (self *state) askToRemove() {
	rows, isRemovable := self.removableList()
	if !isRemovable || self.cursor < 0 || !rows.IsRemovable(self.chosen()) {
		return
	}

	self.removal.index = self.chosen()
}

func (self *state) answerRemoval(keypress key.Key) {
	index := self.removal.index
	self.removal = removal{index: -1}

	if keypress.Code != key.Rune || keypress.Value != 'y' && keypress.Value != 'Y' {
		return
	}

	rows, isRemovable := self.removableList()
	if !isRemovable {
		return
	}
	if err := rows.Remove(index); err != nil {
		self.removal.failure = err.Error()
		return
	}

	at := self.cursor
	self.refilter()
	self.cursor = self.selectableFrom(at)
}

func (self *state) selectableFrom(at int) int {
	for index := max(at, 0); index < len(self.matches); index++ {
		if self.list.IsChoosable(self.matches[index]) {
			return index
		}
	}

	for index := min(at, len(self.matches)) - 1; index >= 0; index-- {
		if self.list.IsChoosable(self.matches[index]) {
			return index
		}
	}

	return -1
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

	wantedIndex := -1
	if self.cursor >= 0 {
		wantedIndex = self.chosen()
	}

	self.query = query
	self.refilter()

	if at := slices.Index(self.matches, wantedIndex); at >= 0 && self.list.IsChoosable(wantedIndex) {
		self.cursor = at
	}
}

func (self *state) refilter() {
	self.matches = self.matches[:0]

	for index := range self.list.Len() {
		if strutil.MatchesQuery(self.list.Text(index), self.query) {
			self.matches = append(self.matches, index)
		}
	}

	self.cursor = self.firstSelectable()
	self.offset = 0
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
	return paint(rows, room, height, cursor, query, false)
}

func PaintRemoval(rows List, room int, height int, cursor int, query string) string {
	return paint(rows, room, height, cursor, query, true)
}

func paint(rows List, room int, height int, cursor int, query string, isRemovalAsked bool) string {
	var screen strings.Builder

	self := newState(rows)
	self.narrow(query)
	self.cursor = cursor
	self.screen = &screen
	self.measure = func() (int, int) { return room, height }
	if isRemovalAsked {
		self.askToRemove()
	}

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
	output.WriteString(self.promptLine(room))
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

func (self *state) promptLine(room int) string {
	if rows, isRemovable := self.removableList(); isRemovable && self.removal.index >= 0 {
		return style.Change(Clip(rows.RemovalPrompt(self.removal.index)+" "+removalAnswers, room))
	}
	if self.removal.failure != "" {
		return style.Failure(Clip(self.removal.failure, room))
	}

	return self.filterLine(room)
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
