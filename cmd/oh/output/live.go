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
	group                  Group    // which group the region holds, so sealing it separates correctly
	topRowIndex            int      // the first row of the region the screen still holds
}

// DrawAnswer replaces the live region with the answer, and reports whether every changed row
// remains on screen.
func (self *Screen) DrawAnswer(rows []string) bool {
	return self.draw(rows, AnswerGroup)
}

// DrawReasoning replaces the live region with the thinking that led to an answer, which runs into
// whatever is written next rather than standing apart from it.
func (self *Screen) DrawReasoning(rows []string) bool {
	return self.draw(rows, AsideGroup)
}

func (self *Screen) draw(newRows []string, next Group) bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if len(newRows) == 0 {
		if len(self.liveRegion.rows) == 0 {
			return true
		}

		newRows = []string{""}
	}

	if len(self.liveRegion.rows) == 0 {
		self.liveRegion.group = next
	}

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
		self.begin(next)
	}

	self.repaint(firstDifference, newRows, next == AnswerGroup)
	self.liveRegion.currentContentRowCount = contentRowCount

	return true
}

func (self *Screen) seal() {
	if len(self.liveRegion.rows) == 0 {
		return
	}

	if !self.isTTY {
		self.begin(self.liveRegion.group)
		self.write(strings.Join(self.liveRegion.rows, "\n"))
	} else if self.liveRegion.currentContentRowCount < len(self.liveRegion.rows) && self.liveRegion.currentContentRowCount > self.liveRegion.topRowIndex {
		rows := slices.Clone(self.liveRegion.rows[:self.liveRegion.currentContentRowCount])
		self.repaint(len(rows)-1, rows, self.liveRegion.group == AnswerGroup)
	}

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
