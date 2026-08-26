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

// BlockHandle is a position-independent handle to a block.
type BlockHandle byte

type groupedBlock struct {
	Block

	group  Group
	handle *BlockHandle
}

// Open puts a work block at the end of the live sequence, under whatever is already open.
func (self *Screen) Open(block Block) {
	self.open(block, WorkGroup, nil)
}

// OpenNotice opens a notice block that its owner may refresh or discard while it remains live.
func (self *Screen) OpenNotice(block Block) *BlockHandle {
	handle := new(BlockHandle)
	self.open(block, NoticeGroup, handle)

	return handle
}

func (self *Screen) open(block Block, group Group, handle *BlockHandle) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if len(self.blocks) == 0 {
		self.seal()
		self.liveRegion.origin = self.drawingState()
		self.liveRegion.hasOrigin = true
		openedRows := self.openedRows

		self.makeRoomFor(group)

		if self.isMidLine {
			self.newline()
		}

		self.openPendingLine()
		self.measureTerminal()
		self.liveRegion.originRowOffset = self.openedRows - openedRows
	}

	self.blocks = append(self.blocks, groupedBlock{Block: block, group: group, handle: handle})

	self.refresh()
}

// RefreshBlock refreshes a block if nothing has since sealed or joined its live region.
func (self *Screen) RefreshBlock(handle *BlockHandle) bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if len(self.blocks) != 1 || self.blocks[0].handle != handle {
		return false
	}

	self.refresh()

	return true
}

// DiscardBlock retracts a block if nothing has since sealed or joined its live region.
func (self *Screen) DiscardBlock(handle *BlockHandle) bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if len(self.blocks) != 1 || self.blocks[0].handle != handle {
		return false
	}

	self.blocks = nil

	return self.discardBlock()
}

// SealBlock seals a block if nothing has since sealed or joined its live region.
func (self *Screen) SealBlock(handle *BlockHandle) bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if len(self.blocks) != 1 || self.blocks[0].handle != handle {
		return false
	}

	self.seal()

	return true
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

	if self.nestedUpdates > 0 {
		self.isLiveDirty = true
		return
	}

	self.paintBlocks()
}

func (self *Screen) flushLiveRegion() {
	if !self.isLiveDirty {
		return
	}

	self.isLiveDirty = false

	if len(self.blocks) > 0 {
		self.paintBlocks()
	}
}

func (self *Screen) paintBlocks() {
	rows, firstGroup, lastGroup := renderGroupedBlocks(self.blocks, self.columns)
	self.paintGroups(rows, firstGroup, lastGroup)
}

func renderGroupedBlocks(blocks []groupedBlock, columns int) ([]string, Group, Group) {
	var rows []string

	for i, grouped := range blocks {
		if i > 0 && blocks[i-1].group != grouped.group {
			rows = append(rows, "")
		}

		rows = append(rows, grouped.Rows(columns)...)
	}

	return rows, blocks[0].group, blocks[len(blocks)-1].group
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
