package output

import (
	"strings"

	"crdx.org/io/cmd/oh/width"
)

type Block interface {
	Rows(columns int) []string
}

type BlockHandle byte

type groupedBlock struct {
	Block

	group  Group
	handle *BlockHandle
}

func (self *Screen) Open(block Block) {
	self.open(block, WorkGroup, nil)
}

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

func (self *Screen) RefreshBlock(handle *BlockHandle) bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if len(self.blocks) != 1 || self.blocks[0].handle != handle {
		return false
	}

	self.refresh()

	return true
}

func (self *Screen) DiscardBlock(handle *BlockHandle) bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if len(self.blocks) != 1 || self.blocks[0].handle != handle {
		return false
	}

	self.blocks = nil

	return self.discardBlock()
}

func (self *Screen) SealBlock(handle *BlockHandle) bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if len(self.blocks) != 1 || self.blocks[0].handle != handle {
		return false
	}

	self.seal()

	return true
}

func (self *Screen) Seal() {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.seal()
}

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

	for i, groupedBlock := range blocks {
		if i > 0 && blocks[i-1].group != groupedBlock.group {
			rows = append(rows, "")
		}

		rows = append(rows, groupedBlock.Rows(columns)...)
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
