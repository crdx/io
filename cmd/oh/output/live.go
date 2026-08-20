package output

import (
	"slices"
	"strings"

	"crdx.org/io/cmd/oh/style"
)

const clearRow = "\x1b[K"

// Draw replaces the separated live region and reports whether every changed row remains on screen.
func (self *Output) Draw(rows []string) bool {
	return self.draw(rows, true)
}

// DrawUnseparated replaces a live region that runs directly into non-prose output.
func (self *Output) DrawUnseparated(rows []string) bool {
	return self.draw(rows, false)
}

func (self *Output) draw(rows []string, separated bool) bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if len(rows) == 0 {
		if len(self.liveRows) == 0 {
			return true
		}

		rows = []string{""}
	}

	if len(self.liveRows) == 0 {
		self.liveSeparated = separated
	}

	if !self.terminal {
		self.liveRows = rows
		self.liveContentRows = len(rows)
		return true
	}

	self.measure()

	contentRows := len(rows)
	if len(rows) < len(self.liveRows) {
		rows = append(slices.Clone(rows), make([]string, len(self.liveRows)-len(rows))...)
	}

	first := firstDifference(self.liveRows, rows)

	if first == len(rows) && first == len(self.liveRows) {
		self.liveContentRows = contentRows
		return true
	}

	if first < self.top {
		return false
	}

	if len(self.liveRows) == 0 {
		self.begin(separated)
	}

	self.repaint(first, rows)
	self.liveContentRows = contentRows

	return true
}

func (self *Output) seal() {
	if len(self.liveRows) == 0 {
		return
	}

	if !self.terminal {
		self.begin(self.liveSeparated)
		self.write(strings.Join(self.liveRows, "\n"))
	} else if self.liveContentRows < len(self.liveRows) && self.liveContentRows > self.top {
		rows := slices.Clone(self.liveRows[:self.liveContentRows])
		self.repaint(len(rows)-1, rows)
	}

	self.liveRows = nil
	self.liveContentRows = 0
	self.liveSeparated = false
	self.top = 0
}

func (self *Output) begin(separated bool) {
	self.separate(separated)
	self.streaming = separated

	if self.midLine {
		self.newline()
	}

	self.openPendingLine()
}

func (self *Output) repaint(first int, rows []string) {
	var out strings.Builder

	out.WriteString(self.openFrame())

	if !self.wrapping {
		self.wrapping = true

		out.WriteString(noAutoWrap)
	}

	out.WriteString(self.eraseInput())

	if back := len(self.liveRows) - 1 - first; back >= 0 {
		out.WriteString(moveUp(back))
	} else if len(self.liveRows) > 0 {
		out.WriteString("\r\n") // nothing on screen changed, so the new rows open a row of their own
	}

	if len(rows) < len(self.liveRows) {
		out.WriteString("\r" + clearBelow)
	}

	for at := first; at < len(rows); at++ {
		if at > first {
			out.WriteString("\r\n")
		}

		out.WriteString("\r" + clearRow)
		out.WriteString(self.linkifyScrollback(rows[at]))
	}

	self.settle(rows)

	out.WriteString(self.drawInput())
	out.WriteString(self.closeFrame())

	self.raw(out.String())
}

func (self *Output) settle(rows []string) {
	self.liveRows = rows
	self.stacked = true
	self.midLine = true
	self.pending = false
	self.trailingNewlines = 0
	self.column = style.Width(rows[len(rows)-1])

	if self.columns > 0 {
		self.column = min(self.column, self.columns)
	}

	if room := self.lines - len(self.input.rows); self.lines > 0 && len(rows) > room {
		self.top = max(self.top, len(rows)-room)
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
