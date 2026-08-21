package output

import (
	"fmt"
	"slices"
	"strings"
)

const (
	up                    = "\x1b[%dA"
	right                 = "\x1b[%dC"
	clearBelow            = "\x1b[J"
	beginFrame            = "\x1b[?2026h" // draw the whole thing before showing any of it
	endFrame              = "\x1b[?2026l"
	hideCursor            = "\x1b[?25l"
	showCursor            = "\x1b[?25h"
	clearScreen           = "\x1b[H\x1b[2J"
	clearScrollback       = "\x1b[3J" // ED2 does not push what it clears into the history, so it goes too
	progressIndeterminate = "\x1b]9;4;3\x1b\\"
	progressClear         = "\x1b]9;4;0\x1b\\"
)

// Synchronise holds every intermediate update back until draw has finished.
func (self *Output) Synchronise(draw func()) {
	if !self.isTerminal {
		draw()
		return
	}

	self.mutex.Lock()
	self.nestedUpdates++
	self.mutex.Unlock()

	defer func() {
		self.mutex.Lock()
		defer self.mutex.Unlock()

		self.nestedUpdates--
		if self.nestedUpdates == 0 {
			text := self.synchronisedBytes.String()
			self.synchronisedBytes.Reset()
			self.writeRaw(beginFrame + hideCursor + text + showCursor + endFrame)
		}
	}()

	draw()
}

func (self *Output) openFrame() string {
	if self.nestedUpdates > 0 {
		return ""
	}

	return beginFrame + hideCursor
}

func (self *Output) closeFrame() string {
	if self.nestedUpdates > 0 {
		return ""
	}

	return showCursor + endFrame
}

type footer struct {
	rows         []string
	cursorRow    int
	cursorColumn int
	column       int  // the conversation column above
	separators   int  // rows between the conversation cursor and the input
	isStacked    bool // whether the input sits below content
}

// Footer draws the input under the conversation, and leaves the cursor sitting in it, which is
// where someone typing expects to find it.
func (self *Output) Footer(rows []string, cursorRow int, cursorColumn int) {
	if !self.isTerminal {
		return
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()

	if slices.Equal(self.input.rows, rows) &&
		self.input.cursorRow == cursorRow && self.input.cursorColumn == cursorColumn {
		return
	}

	self.input = footer{rows: rows, cursorRow: cursorRow, cursorColumn: cursorColumn}

	self.redraw("")
}

// Progress reports whether a turn is running through the terminal progress protocol.
func (self *Output) Progress(isRunning bool) {
	if !self.isTerminal {
		return
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.setProgress(isRunning)
}

func (self *Output) setProgress(isRunning bool) {
	if self.isProgressShown == isRunning {
		return
	}

	sequence := progressClear
	if isRunning {
		sequence = progressIndeterminate
	}

	self.raw(sequence)
	self.isProgressShown = isRunning
}

// Release takes the input away. A kept conversation leaves the cursor on the line below it; an
// unused one is erased so whatever ran the harness can reuse its line.
func (self *Output) Release(shouldKeep bool) {
	if !self.isTerminal {
		return
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.setProgress(false)

	landing := ""
	if self.shownFooter.isStacked {
		if shouldKeep {
			landing = "\r\n"
		} else {
			landing = "\r" + moveUp(self.openedRows) + clearBelow
		}
	}

	self.raw(self.eraseInput() + landing + autoWrap + showCursor)

	self.input = footer{}
	self.openedRows = 0
	self.isWrapping = false
	self.isMidLine = false
	self.hasPendingText = false
}

// Reset clears the terminal and forgets all drawing state for a full replay.
func (self *Output) Reset() {
	if !self.isTerminal {
		return
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.shownFooter = footer{}
	self.input = footer{}
	self.column = 0
	self.openedRows = 0
	self.isMidLine = false
	self.hasPendingText = false
	self.isBlankOwed = false
	self.trailingNewlines = 0
	self.isStreaming = false
	self.isWrapping = false
	self.isStacked = false
	self.liveRegion = liveRegion{}

	self.measure()

	self.raw(clearScreen + clearScrollback)
}

func (self *Output) redraw(text string) {
	var out strings.Builder

	out.WriteString(self.openFrame())

	if !self.isWrapping {
		self.isWrapping = true

		out.WriteString(noAutoWrap)
	}

	out.WriteString(self.eraseInput())
	out.WriteString(text)
	out.WriteString(self.drawInput())
	out.WriteString(self.closeFrame())

	self.raw(out.String())
}

func (self *Output) eraseInput() string {
	shownFooter := self.shownFooter
	self.shownFooter = footer{}

	if len(shownFooter.rows) == 0 {
		return ""
	}

	if !shownFooter.isStacked {
		return "\r" + moveUp(shownFooter.cursorRow) + clearBelow
	}

	return "\r" + moveUp(shownFooter.cursorRow) + clearBelow + moveUp(shownFooter.separators) + moveRight(shownFooter.column)
}

func (self *Output) drawInput() string {
	if len(self.input.rows) == 0 {
		return ""
	}

	self.shownFooter = self.input
	self.shownFooter.column = self.column
	self.shownFooter.isStacked = self.isStacked
	if self.shownFooter.isStacked {
		self.shownFooter.separators = max(0, apart-self.trailingNewlines)
	}

	var out strings.Builder

	for i, row := range self.input.rows {
		switch {
		case i > 0:
			out.WriteString("\r\n")
		case self.shownFooter.separators > 0:
			out.WriteString(strings.Repeat("\r\n", self.shownFooter.separators))
		default:
			out.WriteString("\r")
		}

		out.WriteString(row)
	}

	out.WriteString(moveUp(len(self.input.rows) - 1 - self.input.cursorRow))
	out.WriteString("\r")
	out.WriteString(moveRight(self.input.cursorColumn))

	return out.String()
}

func moveUp(rows int) string {
	if rows <= 0 {
		return ""
	}

	return fmt.Sprintf(up, rows)
}

func moveRight(columns int) string {
	if columns <= 0 {
		return ""
	}

	return fmt.Sprintf(right, columns)
}

// Columns is how wide the terminal is, which is what a line is laid out against.
func (self *Output) Columns() int {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.measure()

	return self.columns
}
