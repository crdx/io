package line

import (
	"strings"

	"crdx.org/io/cmd/oh/key"
)

// Action is what a keypress amounted to, for whoever is driving the input.
type Action int

// What a keypress can amount to.
const (
	Drawn      Action = iota // the line changed, and wants drawing again
	Accept                   // the line is finished
	Cancel                   // escape, or ctrl+d while a turn runs: stop whatever is running
	Quit                     // ctrl+d on an empty line with nothing running
	Write                    // ctrl+x w: swap whether files in the workspace may be changed
	Shell                    // ctrl+x x: swap whether shell commands may run at all
	Git                      // ctrl+x g: swap whether a repository's own history may be changed
	Background               // ctrl+x b: swap whether commands may leave processes behind
	Restart                  // ctrl+r: start the harness again, carrying the conversation over
)

// Input edits a line and walks its history.
type Input struct {
	buffer   *buffer  // the line being edited
	history  *History // the stored entries
	recall   *recall  // the walk through history
	pasting  bool     // whether pasted text is arriving
	prefixed bool     // whether ctrl+x went before, so the next key names a mode
}

// NewInput builds an empty line.
func NewInput(history *History) *Input {
	self := &Input{history: history}
	self.Reset()

	return self
}

// Reset empties the line and starts the walk through history again.
func (self *Input) Reset() {
	self.buffer = &buffer{}
	self.pasting = false
	self.prefixed = false

	if self.history != nil {
		self.recall = self.history.recall()
	}
}

// Text is what has been typed so far.
func (self *Input) Text() string {
	return self.buffer.String()
}

// Pending reports whether a mode prefix awaits its command key.
func (self *Input) Pending() bool {
	return self.prefixed
}

// Frame is the visible input rows, cursor position, and clipped row counts.
type Frame struct {
	Rows   []string // the rows to draw
	Row    int      // the row the cursor is on
	Column int      // the column the cursor is at
	Above  int      // the rows out of sight above
	Below  int      // the rows out of sight below
}

// Frame lays out the current input at width.
func (self *Input) Frame(width int) Frame {
	rows, cursorRow, cursorColumn := layout(self.buffer, width)

	framedRows := window(rows, cursorRow)
	framedRows.Column = cursorColumn

	return framedRows
}

// Apply handles one keypress. Ctrl+D cancels a running turn, quits on an empty idle line, and
// otherwise does nothing.
func (self *Input) Apply(keypress key.Key, running bool) Action {
	if self.pasting {
		self.paste(keypress)
		return Drawn
	}

	if self.prefixed {
		return self.swap(keypress)
	}

	switch keypress.Code {
	case key.Escape:
		return Cancel

	case key.Enter:
		if keypress.Mod.Has(key.Shift) {
			self.buffer.Insert([]rune{'\n'})
			return Drawn
		}

		return Accept

	case key.Left:
		if keypress.Mod.Has(key.Ctrl) {
			self.buffer.MoveWordLeft()
		} else {
			self.buffer.MoveLeft()
		}

	case key.Right:
		if keypress.Mod.Has(key.Ctrl) {
			self.buffer.MoveWordRight()
		} else {
			self.buffer.MoveRight()
		}

	case key.Up:
		if !self.buffer.MoveUp() {
			self.walk(-1)
		}

	case key.Down:
		if !self.buffer.MoveDown() {
			self.walk(1)
		}

	case key.Home:
		self.buffer.MoveHome()

	case key.End:
		self.buffer.MoveEnd()

	case key.Delete:
		self.buffer.DeleteForward()

	case key.Backspace:
		if keypress.Mod.Has(key.Ctrl) {
			self.buffer.DeleteWordBackward()
		} else {
			self.buffer.DeleteBackward()
		}

	case key.PasteStart:
		self.pasting = true

	case key.Rune:
		return self.rune(keypress, running)
	}

	return Drawn
}

const tabStop = 4

func (self *Input) insert(value rune) {
	if value == '\t' {
		self.buffer.Insert([]rune(strings.Repeat(" ", tabStop)))
		return
	}

	self.buffer.Insert([]rune{value})
}

func (self *Input) rune(keypress key.Key, running bool) Action {
	if !keypress.Mod.Has(key.Ctrl) {
		self.insert(keypress.Value)
		return Drawn
	}

	switch keypress.Value {
	case 'c', 'u':
		self.Reset()

	case 'd':
		if running {
			return Cancel
		}

		if self.buffer.Len() == 0 {
			return Quit // and a line with something on it is one ctrl+d has nothing to say about
		}

	case 'r':
		return Restart

	case 'x':
		self.prefixed = true // and the key after it names which mode to swap
	}

	return Drawn
}

func (self *Input) swap(keypress key.Key) Action { // the letters are those in cmd/oh/mode.go
	self.prefixed = false

	if keypress.Code != key.Rune || keypress.Mod != 0 {
		return Drawn
	}

	switch keypress.Value { // and anything else is swallowed rather than typed: a slip is not text
	case 'w':
		return Write

	case 'x':
		return Shell

	case 'g':
		return Git

	case 'b':
		return Background
	}

	return Drawn
}

func (self *Input) paste(keypress key.Key) {
	switch {
	case keypress.Code == key.PasteEnd:
		self.pasting = false
	case keypress.Code == key.Enter:
		self.buffer.Insert([]rune{'\n'})
	case keypress.Code == key.Rune && keypress.Mod == 0:
		self.insert(keypress.Value)
	}
}

func (self *Input) walk(direction int) {
	if self.recall == nil {
		return
	}

	if line, found := self.recall.Walk(self.buffer.String(), direction); found {
		self.buffer.Set(line)
	}
}
