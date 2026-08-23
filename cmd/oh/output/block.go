package output

import (
	"strings"

	"crdx.org/io/cmd/oh/width"
)

// Block is a run of rows in the live region. The screen asks a block what it looks like whenever
// anything in the region changes, so a block never draws itself and never owns where it sits.
type Block interface {
	Rows(columns int) []string
}

// Open puts a block at the end of the live sequence, under whatever is already open.
func (self *Screen) Open(block Block) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if len(self.blocks) == 0 {
		self.seal()
		self.makeRoomFor(AsideGroup)

		if self.isMidLine {
			self.newline()
		}

		self.openPendingLine()
		self.measureTerminal()
	}

	self.blocks = append(self.blocks, block)

	self.refresh()
}

// Seal ends the live sequence, leaving what it last drew behind as scrollback.
func (self *Screen) Seal() {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.seal()
}

// Refresh draws the live sequence again, which is how a block says it has changed.
func (self *Screen) Refresh() {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.refresh()
}

func (self *Screen) refresh() {
	if len(self.blocks) == 0 {
		return
	}

	var rows []string

	for at, block := range self.blocks {
		if at > 0 {
			rows = append(rows, "")
		}

		rows = append(rows, block.Rows(self.columns)...)
	}

	self.paint(rows, AsideGroup)
}

type textBlock struct {
	text string
}

func (self textBlock) Rows(columns int) []string {
	var rows []string

	for line := range strings.SplitSeq(self.text, "\n") {
		if columns <= 0 {
			rows = append(rows, line)
			continue
		}

		rows = append(rows, width.Wrap(line, columns)...)
	}

	return rows
}
