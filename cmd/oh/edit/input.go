package edit

import (
	"strings"
	"time"

	"crdx.org/io/cmd/oh/key"
)

// —————————————————————————————————————————————————————————————————————————————————————————————————
// mega:allow-file comment-inlines
// —————————————————————————————————————————————————————————————————————————————————————————————————

// Action is what a keypress amounted to, for whoever is driving the input.
type Action int

// What a keypress can amount to.
const (
	Draw        Action = iota // the line changed, and wants drawing again
	Accept                    // the line is finished
	ForceAccept               // alt+enter: send the line without interpreting a command
	Continue                  // double enter on an empty line: send the get-on-with-it message
	Cancel                    // escape, or ctrl+d while a turn runs: stop whatever is running
	Quit                      // ctrl+d on an empty line with nothing running
	ToggleWrite               // ctrl+x w: swap whether files in the workspace may be changed
	ToggleShell               // ctrl+x x: swap whether shell commands may run at all
	ToggleGit                 // ctrl+x g: swap whether a repository's own history may be changed
	ToggleWeb                 // ctrl+x s: swap whether the web tools may reach the internet
	Complete                  // tab: complete a slash command when exactly one name matches
)

// Input edits a line and walks its history.
type Input struct {
	buffer     *Buffer
	history    *History
	recall     *Recall
	search     *historySearch
	frameWidth int

	isPasting       bool
	pasteStart      int              // where the current paste begins in the buffer
	isPrefixPending bool             // whether ctrl+x went before, so the next key names a mode
	isEnterPending  bool             // whether one enter awaits a second before sending
	isClearPending  bool             // whether one ctrl+c awaits a second before wiping
	acceptAfter     time.Time        // when another return may accept the line
	continueAfter   time.Time        // when another double enter may continue
	currentTime     func() time.Time // supplies the time for the enter cool-offs
	wasRunning      bool             // whether a turn ran when the previous key was applied
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
	self.search = nil
	self.isPasting = false
	self.isPrefixPending = false
	self.isEnterPending = false
	self.isClearPending = false
	self.acceptAfter = time.Time{}
	self.wasRunning = false

	if self.history != nil {
		self.recall = self.history.recall()
	}
}

func (self *Input) Text() string {
	return self.buffer.String()
}

func (self *Input) SetText(text string) {
	self.finishSearch()
	self.buffer.Set(text)
}

func (self *Input) IsPasting() bool {
	return self.isPasting
}

func (self *Input) IsPrefixPending() bool {
	return self.isPrefixPending
}

type Frame struct {
	Rows             []string
	Row              int
	Column           int
	IsSearching      bool
	SearchQuery      string
	HiddenLinesAbove int // the rows out of sight above
	HiddenLinesBelow int // the rows out of sight below
}

func (self *Input) Frame(width int) Frame {
	self.frameWidth = width
	rows, cursorRow, cursorColumn := layout(self.buffer, width)

	framedRows := window(rows, cursorRow)
	framedRows.Column = cursorColumn
	if self.search != nil {
		framedRows.IsSearching = true
		framedRows.SearchQuery = self.search.getQuery()
	}

	return framedRows
}

func (self *Input) Apply(keypress key.Key, isRunning bool) Action {
	if self.wasRunning != isRunning {
		self.isEnterPending = false
	}
	self.wasRunning = isRunning

	if self.applyClearKey(keypress) {
		return Draw
	}

	self.isClearPending = false

	if self.isPasting {
		self.isEnterPending = false
		self.paste(keypress)
		return Draw
	}

	if self.applySearchKey(keypress) {
		return Draw
	}

	if self.applyReadlineKey(keypress) {
		return Draw
	}

	if keypress.Code != key.Enter || keypress.Mod.Has(key.Shift) {
		self.isEnterPending = false
	}

	if self.isPrefixPending {
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
		if !moveCursorVertically(self.buffer, self.frameWidth, -1) {
			self.walk(-1)
		}

	case key.Down:
		if !moveCursorVertically(self.buffer, self.frameWidth, 1) {
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
		return self.rune(keypress, isRunning)

	case key.PageUp, key.PageDown, key.PasteEnd, key.FocusIn, key.FocusOut, key.Unknown:
	}

	return Draw
}

func (self *Input) applyClearKey(keypress key.Key) bool {
	if keypress.Code != key.Rune || !keypress.Mod.Has(key.Ctrl) {
		return false
	}

	switch keypress.Value {
	case 'u':
		self.Reset()
		return true
	case 'c':
		if self.buffer.Len() > 0 && !self.isClearPending {
			self.isClearPending = true
			return true
		}

		self.Reset()
		return true
	}

	return false
}

func (self *Input) applyReadlineKey(keypress key.Key) bool {
	if keypress.Code != key.Rune || !keypress.Mod.Has(key.Ctrl) {
		return false
	}

	switch keypress.Value {
	case 'a':
		self.buffer.MoveHome()
	case 'b':
		self.buffer.MoveLeft()
	case 'e':
		self.buffer.MoveEnd()
	case 'f':
		self.buffer.MoveRight()
	case 'k':
		self.buffer.DeleteToEnd()
	case 'r':
		self.startSearch()
	case 'w':
		self.buffer.DeleteWhitespaceWordBackward()
	default:
		return false
	}

	return true
}

func (self *Input) applySearchKey(keypress key.Key) bool {
	if self.search == nil {
		return false
	}

	switch {
	case keypress.Code == key.Rune && keypress.Value == 'r' && keypress.Mod.Has(key.Ctrl):
		self.search.previous()
		self.buffer.Set(self.search.getText())
		return true
	case keypress.Code == key.Rune && keypress.Mod == 0:
		self.search.add(keypress.Value)
		self.buffer.Set(self.search.getText())
		return true
	case keypress.Code == key.Backspace && keypress.Mod == 0:
		self.search.deleteBackward()
		self.buffer.Set(self.search.getText())
		return true
	case keypress.Code == key.Escape:
		self.finishSearch()
		return true
	default:
		self.finishSearch()
		return false
	}
}

func (self *Input) startSearch() {
	if self.history == nil {
		return
	}

	self.search = self.history.search(self.buffer.String())
}

func (self *Input) finishSearch() {
	if self.search == nil {
		return
	}

	self.recall = self.search.recall()
	self.search = nil
}

const (
	acceptCoolOff   = 250 * time.Millisecond
	continueCoolOff = time.Second
)

func (self *Input) enter(keypress key.Key) Action {
	if keypress.Mod.Has(key.Shift) {
		self.buffer.Insert([]rune{'\n'})
		return Draw
	}

	if strings.TrimSpace(self.buffer.String()) != "" {
		now := self.currentTime()
		isCoolingOff := now.Before(self.acceptAfter)
		self.acceptAfter = now.Add(acceptCoolOff)

		switch {
		case isCoolingOff:
			return Draw
		case keypress.Mod.Has(key.Alt):
			return ForceAccept
		default:
			return Accept
		}
	}

	now := self.currentTime()
	if now.Before(self.continueAfter) {
		self.continueAfter = now.Add(continueCoolOff)
		return Draw
	}

	if self.isEnterPending {
		self.isEnterPending = false
		self.continueAfter = now.Add(continueCoolOff)
		return Continue
	}

	self.isEnterPending = true
	return Draw
}

const tabStop = 4

func (self *Input) insert(value rune) {
	if value == '\t' {
		self.buffer.Insert([]rune(strings.Repeat(" ", tabStop)))
		return
	}

	self.buffer.Insert([]rune{value})
}

func (self *Input) rune(keypress key.Key, isRunning bool) Action {
	if !keypress.Mod.Has(key.Ctrl) {
		if keypress.Value == '\t' && strings.HasPrefix(self.buffer.String(), "/") {
			return Complete
		}
		self.insert(keypress.Value)
		return Draw
	}

	switch keypress.Value {
	case 'd':
		if isRunning {
			return Cancel
		}

		if self.buffer.Len() == 0 {
			return Quit
		}

	case 'x':
		self.isPrefixPending = true
	}

	return Draw
}

func (self *Input) toggleMode(button key.Key) Action {
	self.isPrefixPending = false

	if button.Code != key.Rune || button.Mod != 0 {
		return Draw
	}

	switch button.Value {
	case 'w':
		return ToggleWrite

	case 'x':
		return ToggleShell

	case 'g':
		return ToggleGit

	case 's':
		return ToggleWeb
	}

	return Draw
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
	pastedText := string(self.buffer.Runes()[self.pasteStart:end])
	lines := strings.Split(pastedText, "\n")
	indentation := len(pastedText)

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}

		indentation = min(indentation, len(line)-len(strings.TrimLeft(line, " ")))
	}

	if indentation == 0 || indentation == len(pastedText) {
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
