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
	beginFrame            = "\x1b[?2026h"
	endFrame              = "\x1b[?2026l"
	hideCursor            = "\x1b[?25l"
	showCursor            = "\x1b[?25h"
	barCursor             = "\x1b[5 q"
	defaultCursor         = "\x1b[0 q"
	clearScreen           = "\x1b[H\x1b[2J"
	clearScrollback       = "\x1b[3J"
	progressIndeterminate = "\x1b]9;4;3\x1b\\"
	progressClear         = "\x1b]9;4;0\x1b\\"
)

func (self *Screen) BeginEditing() func() {
	if !self.isTTY {
		return func() {}
	}

	self.writeRaw(barCursor)
	return func() { self.writeRaw(defaultCursor) }
}

func (self *Screen) Sync(draw func()) {
	if !self.isTTY {
		draw()
		return
	}

	self.mutex.Lock()
	self.nestedUpdates++
	self.mutex.Unlock()

	defer func() {
		self.mutex.Lock()
		defer self.mutex.Unlock()

		if self.nestedUpdates == 1 {
			self.flushLiveRegion()
		}

		self.nestedUpdates--
		if self.nestedUpdates == 0 {
			text := self.synchronisedBytes.String()
			self.synchronisedBytes.Reset()

			if text != "" {
				self.writeRaw(beginFrame + hideCursor + text + showCursor + endFrame)
			}
		}
	}()

	draw()
}

func (self *Screen) openFrame() string {
	if self.nestedUpdates > 0 {
		return ""
	}

	return beginFrame + hideCursor
}

func (self *Screen) closeFrame() string {
	if self.nestedUpdates > 0 {
		return ""
	}

	return showCursor + endFrame
}

type footer struct {
	rows            []string
	cursorRow       int
	cursorColumn    int
	column          int
	separators      int
	hasContentAbove bool
}

func (self *Screen) Footer(rows []string, cursorRow int, cursorColumn int) {
	if !self.isTTY {
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

func (self *Screen) WriteEscape(escape string) bool {
	if !self.isTTY {
		return false
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.raw(escape)

	return true
}

func (self *Screen) ReportProgress(isRunning bool) {
	if !self.isTTY {
		return
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.setProgress(isRunning)
}

func (self *Screen) RefreshProgress() {
	if !self.isTTY {
		return
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()

	if self.isProgressReported {
		self.raw(progressIndeterminate)
	}
}

func (self *Screen) setProgress(isRunning bool) {
	if self.isProgressReported == isRunning {
		return
	}

	sequence := progressClear
	if isRunning {
		sequence = progressIndeterminate
	}

	self.raw(sequence)
	self.isProgressReported = isRunning
}

func (self *Screen) Release(shouldKeep bool) {
	if !self.isTTY {
		return
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.setProgress(false)

	landing := ""
	if self.shownFooter.hasContentAbove {
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

func (self *Screen) Reset() {
	if !self.isTTY {
		return
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.shownFooter = footer{}
	self.input = footer{}
	self.blocks = nil
	self.column = 0
	self.openedRows = 0
	self.isMidLine = false
	self.hasPendingText = false
	self.isBlankOwed = false
	self.trailingNewlines = 0
	self.lastGroup = NoticeGroup
	self.isWrapping = false
	self.hasPrinted = false
	self.liveRegion = liveRegion{}
	self.isLiveDirty = false

	self.measureTerminal()

	self.raw(clearScreen + clearScrollback)
}

func (self *Screen) redraw(text string) {
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

func (self *Screen) eraseInput() string {
	shownFooter := self.shownFooter
	self.shownFooter = footer{}

	if len(shownFooter.rows) == 0 {
		return ""
	}

	if !shownFooter.hasContentAbove {
		return "\r" + moveUp(shownFooter.cursorRow) + clearBelow
	}

	return "\r" + moveUp(shownFooter.cursorRow) + clearBelow + moveUp(shownFooter.separators) + moveRight(shownFooter.column)
}

func (self *Screen) drawInput() string {
	if len(self.input.rows) == 0 {
		return ""
	}

	self.shownFooter = self.input
	self.shownFooter.column = self.column
	self.shownFooter.hasContentAbove = self.hasPrinted
	if self.shownFooter.hasContentAbove {
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

func (self *Screen) Columns() int {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.measureTerminal()

	return self.columns
}
