package output

import (
	"io"
	"os"
	"strings"
	"sync"

	"crdx.org/io/cmd/oh/link"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/oh/tty"

	"golang.org/x/term"
)

// Screen serializes terminal drawing.
type Screen struct {
	writer io.Writer

	mutex sync.Mutex // guards drawing

	isMidLine        bool  // whether the cursor follows text on the current line
	hasPendingText   bool  // whether streamed text has not ended in a newline
	isBlankOwed      bool  // whether an empty line is owed to whatever is written next
	trailingNewlines int   // how many newlines the last thing written ended with
	lastGroup        Group // what was written last, so that a change of group opens a blank line
	hasPrinted       bool  // whether anything has reached the screen yet, latched by emit until Reset

	isTTY           bool   // whether the writer is a terminal rather than a file or a pipe
	linkRoot        string // where relative paths drawn in the scrollback begin, and "" to link nothing
	isProgressShown bool   // whether a turn is reported as running to the terminal

	columns    int // the terminal width
	lines      int // the terminal height
	column     int // the cursor column
	openedRows int // how many conversation rows precede the current one

	isWrapping        bool            // whether the terminal wraps at its edge
	nestedUpdates     int             // how many whole-screen updates are holding their intermediate frames back
	synchronisedBytes strings.Builder // output withheld until the outer synchronised update is complete

	input       footer // what the input should look like
	shownFooter footer // what is on the screen

	liveRegion  liveRegion     // the rows being repainted in place
	isLiveDirty bool           // whether a deferred refresh is owed
	blocks      []groupedBlock // the blocks the live region is made of, in the order they opened
}

// New builds the output over a writer, which is a terminal or is not.
func New(writer io.Writer) *Screen {
	self := &Screen{writer: writer, isTTY: tty.Is(writer)}

	self.measureTerminal()

	return self
}

// NewTerminalOfSize builds an Output that is drawn as a terminal of the given size.
func NewTerminalOfSize(writer io.Writer, columns int, lines int) *Screen {
	return &Screen{
		writer:  writer,
		isTTY:   true,
		columns: columns,
		lines:   lines,
	}
}

// LinkPathsUnder marks the paths drawn text names as terminal hyperlinks, resolving the relative
// ones against root. Nothing is linked until it is given one.
func (self *Screen) LinkPathsUnder(root string) *Screen {
	self.linkRoot = root
	return self
}

// Line writes text on a line of its own after any streamed answer. Where blocks are open, the text
// joins the sequence as a block of its own rather than disturbing them.
func (self *Screen) Line(text string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if len(self.blocks) > 0 {
		self.blocks = append(self.blocks, groupedBlock{Block: textBlock{text: text}, group: NoticeGroup})
		self.refresh()

		return
	}

	self.seal()
	self.makeRoomFor(NoticeGroup)

	if self.isMidLine {
		self.newline()
	}

	self.write(text)
}

// Blank schedules one empty line before the next output. Repeated calls coalesce.
func (self *Screen) Blank() {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.isBlankOwed = self.hasPrinted
}

// End finishes the turn on a complete line. Repeated calls are inert.
func (self *Screen) End() {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.seal()

	if self.isMidLine {
		self.newline()
		self.isMidLine = false
		self.hasPendingText = false
	}
}

func (self *Screen) drawRow(text string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.makeRoomFor(NoticeGroup)

	self.write(text)
}

func (self *Screen) measureTerminal() {
	file, ok := self.writer.(*os.File)
	if !ok {
		return
	}

	if columns, lines, err := term.GetSize(int(file.Fd())); err == nil {
		self.columns = columns
		self.lines = lines
	}
}

func (self *Screen) newline() {
	self.emit("\n")
}

func (self *Screen) write(text string) {
	if text == "" {
		return
	}

	self.openPendingLine()
	self.emit(text)
}

func (self *Screen) emit(text string) {
	self.hasPrinted = true
	fitted := self.fit(text)
	self.advance(text)
	self.count(text)
	self.at(fitted)
}

const apart = 2

func (self *Screen) makeRoomFor(next Group) {
	if self.lastGroup != next {
		self.isBlankOwed = self.hasPrinted
	}

	self.lastGroup = next
}

func (self *Screen) openPendingLine() {
	if self.hasPendingText {
		self.hasPendingText = false
		self.newline()
	}

	if self.isBlankOwed {
		self.isBlankOwed = false

		for self.trailingNewlines < apart {
			self.newline()
		}
	}
}

func (self *Screen) advance(text string) {
	if index := strings.LastIndex(text, "\n"); index >= 0 {
		self.isMidLine = false
		text = text[index+1:]
	}

	if text != "" {
		self.isMidLine = true
	}
}

func (self *Screen) count(styledText string) {
	text := style.Plain(styledText)
	trailingNewlines := len(text) - len(strings.TrimRight(text, "\n"))

	if trailingNewlines == len(text) {
		self.trailingNewlines += trailingNewlines
		return
	}

	self.trailingNewlines = trailingNewlines
}

func (self *Screen) at(text string) {
	if len(self.shownFooter.rows) == 0 {
		self.raw(text)
		return
	}

	self.redraw(text)
}

func (self *Screen) linkifyScrollback(text string) string {
	if !self.isTTY || self.linkRoot == "" {
		return text
	}

	return link.Render(text, self.linkRoot)
}

func (self *Screen) raw(text string) {
	if self.nestedUpdates > 0 {
		self.synchronisedBytes.WriteString(text)
		return
	}

	self.writeRaw(text)
}

func (self *Screen) writeRaw(text string) {
	_, _ = io.WriteString(self.writer, text)
}
