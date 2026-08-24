package output

import (
	"slices"
	"strings"

	"crdx.org/io/cmd/oh/style"
)

const clearRow = "\x1b[K"

type liveRegion struct {
	rows                   []string // the rows as they were last painted
	currentContentRowCount int      // how many of them are content rather than height-preserving blanks
	firstGroup             Group    // the group at the start of the region
	lastGroup              Group    // the group at the end of the region
	topRowIndex            int      // the first row of the region the screen still holds
}

// DrawAnswer replaces the live region with the answer, and reports whether every changed row
// remains on screen.
func (self *Screen) DrawAnswer(rows []string) bool {
	return self.draw(rows, AnswerGroup)
}

// DrawReasoning replaces the live region with the thinking that led to an answer.
func (self *Screen) DrawReasoning(rows []string) bool {
	return self.draw(rows, WorkGroup)
}

// DiscardLive erases the unsealed live region without leaving it in scrollback.
func (self *Screen) DiscardLive() bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.blocks = nil
	if len(self.liveRegion.rows) == 0 {
		return true
	}
	if self.liveRegion.topRowIndex > 0 {
		return false
	}
	if self.isTTY {
		self.repaint(0, []string{""}, false)
		self.liveRegion = liveRegion{}
		return false
	}
	self.liveRegion = liveRegion{}

	return true
}

func (self *Screen) draw(newRows []string, next Group) bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if len(self.blocks) > 0 {
		self.seal()
	}

	return self.paint(newRows, next)
}

func (self *Screen) paint(newRows []string, group Group) bool {
	return self.paintGroups(newRows, group, group)
}

func (self *Screen) paintGroups(newRows []string, firstGroup Group, lastGroup Group) bool {
	if len(newRows) == 0 {
		if len(self.liveRegion.rows) == 0 {
			return true
		}

		newRows = []string{""}
	}

	if len(self.liveRegion.rows) == 0 {
		self.liveRegion.firstGroup = firstGroup
	}
	self.liveRegion.lastGroup = lastGroup

	if !self.isTTY {
		self.liveRegion.rows = newRows
		self.liveRegion.currentContentRowCount = len(newRows)
		return true
	}

	self.measureTerminal()

	contentRowCount := len(newRows)
	if len(newRows) < len(self.liveRegion.rows) {
		newRows = append(slices.Clone(newRows), make([]string, len(self.liveRegion.rows)-len(newRows))...)
	}

	firstDifference := getFirstDifference(self.liveRegion.rows, newRows)

	if firstDifference == len(newRows) && firstDifference == len(self.liveRegion.rows) {
		self.liveRegion.currentContentRowCount = contentRowCount
		return true
	}

	if firstDifference < self.liveRegion.topRowIndex {
		return false
	}

	if len(self.liveRegion.rows) == 0 {
		self.begin(firstGroup)
	}

	self.repaint(firstDifference, newRows, firstGroup == AnswerGroup && lastGroup == AnswerGroup)
	self.liveRegion.currentContentRowCount = contentRowCount
	self.lastGroup = lastGroup

	return true
}

func (self *Screen) seal() {
	self.blocks = nil

	if len(self.liveRegion.rows) == 0 {
		return
	}

	if !self.isTTY {
		self.begin(self.liveRegion.firstGroup)
		self.write(strings.Join(self.liveRegion.rows, "\n"))
	} else if self.liveRegion.currentContentRowCount < len(self.liveRegion.rows) && self.liveRegion.currentContentRowCount > self.liveRegion.topRowIndex {
		rows := slices.Clone(self.liveRegion.rows[:self.liveRegion.currentContentRowCount])
		self.repaint(len(rows)-1, rows, self.liveRegion.firstGroup == AnswerGroup && self.liveRegion.lastGroup == AnswerGroup)
	}

	self.lastGroup = self.liveRegion.lastGroup
	self.liveRegion = liveRegion{}
}

func (self *Screen) begin(next Group) {
	self.makeRoomFor(next)

	if self.isMidLine {
		self.newline()
	}

	self.openPendingLine()
}

func (self *Screen) repaint(first int, rows []string, shouldLinkPaths bool) {
	var out strings.Builder

	out.WriteString(self.openFrame())

	if !self.isWrapping {
		self.isWrapping = true

		out.WriteString(noAutoWrap)
	}

	out.WriteString(self.eraseInput())

	if back := len(self.liveRegion.rows) - 1 - first; back >= 0 {
		out.WriteString(moveUp(back))
	} else if len(self.liveRegion.rows) > 0 {
		out.WriteString("\r\n")
	}

	if len(rows) < len(self.liveRegion.rows) {
		out.WriteString("\r" + clearBelow)
	}

	for at := first; at < len(rows); at++ {
		if at > first {
			out.WriteString("\r\n")
		}

		row := rows[at]
		if shouldLinkPaths {
			row = self.linkifyScrollback(row)
		}

		out.WriteString("\r" + clearRow)
		out.WriteString(row)
	}

	self.settle(rows)

	out.WriteString(self.drawInput())
	out.WriteString(self.closeFrame())

	self.raw(out.String())
}

func (self *Screen) settle(rows []string) {
	self.liveRegion.rows = rows
	self.hasPrinted = true
	self.isMidLine = true
	self.hasPendingText = false
	self.trailingNewlines = 0
	self.column = style.Width(rows[len(rows)-1])

	if self.columns > 0 {
		self.column = min(self.column, self.columns)
	}

	if room := self.lines - len(self.input.rows); self.lines > 0 && len(rows) > room {
		self.liveRegion.topRowIndex = max(self.liveRegion.topRowIndex, len(rows)-room)
	}
}

func getFirstDifference(before []string, after []string) int {
	for at := range min(len(before), len(after)) {
		if before[at] != after[at] {
			return at
		}
	}

	return min(len(before), len(after))
}
