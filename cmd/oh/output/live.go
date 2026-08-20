package output

import (
	"slices"
	"strings"

	"crdx.org/io/cmd/oh/style"
)

const clearRow = "\x1b[K"

type liveRegion struct {
	rows        []string // the rows as they were last painted
	contentRows int      // how many of them are content rather than height-preserving blanks
	isAnswer    bool     // whether the region holds the answer, and not the thinking before it
	top         int      // the first row of the region the screen still holds
}

// DrawAnswer replaces the live region with the answer, and reports whether every changed row
// remains on screen.
func (self *Output) DrawAnswer(rows []string) bool {
	return self.draw(rows, true)
}

// DrawReasoning replaces the live region with the thinking that led to an answer, which runs into
// whatever is written next rather than standing apart from it.
func (self *Output) DrawReasoning(rows []string) bool {
	return self.draw(rows, false)
}

func (self *Output) draw(rows []string, isAnswer bool) bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if len(rows) == 0 {
		if len(self.liveRegion.rows) == 0 {
			return true
		}

		rows = []string{""}
	}

	if len(self.liveRegion.rows) == 0 {
		self.liveRegion.isAnswer = isAnswer
	}

	if !self.isTerminal {
		self.liveRegion.rows = rows
		self.liveRegion.contentRows = len(rows)
		return true
	}

	self.measure()

	contentRows := len(rows)
	if len(rows) < len(self.liveRegion.rows) {
		rows = append(slices.Clone(rows), make([]string, len(self.liveRegion.rows)-len(rows))...)
	}

	first := firstDifference(self.liveRegion.rows, rows)

	if first == len(rows) && first == len(self.liveRegion.rows) {
		self.liveRegion.contentRows = contentRows
		return true
	}

	if first < self.liveRegion.top {
		return false
	}

	if len(self.liveRegion.rows) == 0 {
		self.begin(isAnswer)
	}

	self.repaint(first, rows, isAnswer)
	self.liveRegion.contentRows = contentRows

	return true
}

func (self *Output) seal() {
	if len(self.liveRegion.rows) == 0 {
		return
	}

	if !self.isTerminal {
		self.begin(self.liveRegion.isAnswer)
		self.write(strings.Join(self.liveRegion.rows, "\n"))
	} else if self.liveRegion.contentRows < len(self.liveRegion.rows) && self.liveRegion.contentRows > self.liveRegion.top {
		rows := slices.Clone(self.liveRegion.rows[:self.liveRegion.contentRows])
		self.repaint(len(rows)-1, rows, self.liveRegion.isAnswer)
	}

	self.liveRegion = liveRegion{}
}

func (self *Output) begin(isAnswer bool) {
	self.separate(isAnswer)
	self.isStreaming = isAnswer

	if self.isMidLine {
		self.newline()
	}

	self.openPendingLine()
}

func (self *Output) repaint(first int, rows []string, shouldLinkPaths bool) {
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
		out.WriteString("\r\n") // nothing on screen changed, so the new rows open a row of their own
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

func (self *Output) settle(rows []string) {
	self.liveRegion.rows = rows
	self.isStacked = true
	self.isMidLine = true
	self.hasPendingText = false
	self.trailingNewlines = 0
	self.column = style.Width(rows[len(rows)-1])

	if self.columns > 0 {
		self.column = min(self.column, self.columns)
	}

	if room := self.lines - len(self.input.rows); self.lines > 0 && len(rows) > room {
		self.liveRegion.top = max(self.liveRegion.top, len(rows)-room)
	}
}

func firstDifference(before []string, after []string) int {
	for at := range min(len(before), len(after)) {
		if before[at] != after[at] {
			return at
		}
	}

	return min(len(before), len(after))
}
