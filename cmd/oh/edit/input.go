// Package edit is the line being typed: the buffer under it, the history behind it, what each
// keypress amounts to, and how the text lays out at a given width. It draws none of it, and hands
// out a Frame for line to draw.
package edit

import (
	"strings"
	"time"

	"crdx.org/io/cmd/oh/key"
)

// Action is what a keypress amounted to, for whoever is driving the input.
type Action int

// What a keypress can amount to.
const (
	Drawn      Action = iota // the line changed, and wants drawing again
	Accept                   // the line is finished
	Continue                 // double enter on an empty line: send the get-on-with-it message
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
	buffer         *Buffer
	history        *History
	recall         *Recall
	isPasting      bool
	pasteStart     int              // where the current paste begins in the buffer
	inChord        bool             // whether ctrl+x went before, so the next key names a mode
	isEnterPending bool             // whether one enter awaits a second
	continueAfter  time.Time        // when another double enter may continue
	currentTime    func() time.Time // supplies the time for the continuation cool-off
	wasRunning     bool             // whether a turn ran when the previous key was applied
}

// NewInput builds an empty line.
func NewInput(history *History) *Input {
	self := &Input{
		history:     history,
		currentTime: time.Now,
	}
	self.Reset()

	return self
}

// Reset empties the line and starts the walk through history again.
func (self *Input) Reset() {
	self.buffer = &Buffer{}
	self.isPasting = false
	self.inChord = false
	self.isEnterPending = false
	self.wasRunning = false

	if self.history != nil {
		self.recall = self.history.recall()
	}
}

// Text is what has been typed so far.
func (self *Input) Text() string {
	return self.buffer.String()
}

// IsPending reports whether a mode prefix awaits its command key.
func (self *Input) IsPending() bool {
	return self.inChord
}

// Frame is the visible input rows, cursor position, and clipped row counts.
type Frame struct {
	Rows   []string
	Row    int
	Column int
	Above  int // the rows out of sight above
	Below  int // the rows out of sight below
}

// Frame lays out the current input at width.
func (self *Input) Frame(width int) Frame {
	rows, cursorRow, cursorColumn := layout(self.buffer, width)

	framedRows := window(rows, cursorRow)
	framedRows.Column = cursorColumn

	return framedRows
}

// Apply handles one keypress.
func (self *Input) Apply(keypress key.Key, running bool) Action {
	if self.wasRunning != running {
		self.isEnterPending = false
	}
	self.wasRunning = running

	if keypress.Code == key.Rune && keypress.Mod.Has(key.Ctrl) &&
		(keypress.Value == 'c' || keypress.Value == 'u') {
		self.Reset()
		return Drawn
	}

	if self.isPasting {
		self.isEnterPending = false
		self.paste(keypress)
		return Drawn
	}

	if keypress.Code != key.Enter || keypress.Mod.Has(key.Shift) {
		self.isEnterPending = false
	}

	if self.inChord {
		return self.toggleMode(keypress)
	}

	switch keypress.Code {
	case key.Escape:
		return Cancel

	case key.Enter:
		return self.enter(keypress)

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
		self.isPasting = true
		self.pasteStart = self.buffer.Cursor()

	case key.Rune:
		return self.rune(keypress, running)
	}

	return Drawn
}

const continueCoolOff = time.Second

func (self *Input) enter(keypress key.Key) Action {
	if keypress.Mod.Has(key.Shift) {
		self.buffer.Insert([]rune{'\n'})
		return Drawn
	}

	if strings.TrimSpace(self.buffer.String()) != "" {
		return Accept
	}

	now := self.currentTime()
	if now.Before(self.continueAfter) {
		self.continueAfter = now.Add(continueCoolOff)
		return Drawn
	}

	if self.isEnterPending {
		self.isEnterPending = false
		self.continueAfter = now.Add(continueCoolOff)
		return Continue
	}

	self.isEnterPending = true
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
	case 'd':
		if running {
			return Cancel
		}

		if self.buffer.Len() == 0 {
			return Quit
		}

	case 'r':
		return Restart

	case 'x':
		self.inChord = true
	}

	return Drawn
}

func (self *Input) toggleMode(button key.Key) Action {
	self.inChord = false

	if button.Code != key.Rune || button.Mod != 0 {
		return Drawn
	}

	switch button.Value {
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
		self.isPasting = false
		self.normalisePasteIndentation()
	case keypress.Code == key.Enter:
		self.buffer.Insert([]rune{'\n'})
	case keypress.Code == key.Rune && keypress.Mod == 0:
		self.insert(keypress.Value)
	}
}

func (self *Input) normalisePasteIndentation() {
	end := self.buffer.Cursor()
	pasted := string(self.buffer.Runes()[self.pasteStart:end])
	lines := strings.Split(pasted, "\n")
	indentation := len(pasted)

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}

		indentation = min(indentation, len(line)-len(strings.TrimLeft(line, " ")))
	}

	if indentation == 0 || indentation == len(pasted) {
		return
	}

	for i, line := range lines {
		leadingSpaces := len(line) - len(strings.TrimLeft(line, " "))
		lines[i] = line[min(indentation, leadingSpaces):]
	}

	self.buffer.remove(self.pasteStart, end)
	self.buffer.Insert([]rune(strings.Join(lines, "\n")))
}

func (self *Input) walk(direction int) {
	if self.recall == nil {
		return
	}

	if line, found := self.recall.Walk(self.buffer.String(), direction); found {
		self.buffer.Set(line)
	}
}
