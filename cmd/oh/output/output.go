package output

import (
	"io"
	"os"
	"strings"
	"sync"

	"crdx.org/io/cmd/oh/pathlink"
	"crdx.org/io/cmd/oh/status"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/oh/tty"

	"golang.org/x/term"
)

// Output serializes terminal drawing.
type Output struct {
	writer io.Writer

	mutex sync.Mutex // guards drawing

	isMidLine        bool   // whether the cursor follows text on the current line
	hasPendingText   bool   // whether streamed text has not ended in a newline
	isBlankOwed      bool   // whether an empty line is owed to whatever is written next
	trailingNewlines int    // how many newlines the last thing written ended with
	hoardedNewlines  string // the newlines an answer ended on, kept back in case it goes on
	isStreaming      bool   // whether an answer is arriving in pieces
	isStacked        bool   // whether anything has been said, and so whether the input has a row to sit under

	isTerminal      bool
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

	liveRows        []string // the rows of the live region as they were last painted
	liveContentRows int      // how many live rows are content rather than height-preserving blanks
	liveAnswer      bool     // whether the live region holds the answer, and not the thinking before it
	top             int      // the first row of the region the screen still holds
}

// New builds the output over a writer, which is a terminal or is not.
func New(writer io.Writer) *Output {
	self := &Output{writer: writer, isTerminal: tty.Is(writer)}

	self.measure()

	return self
}

// LinkPathsUnder marks the paths drawn text names as terminal hyperlinks, resolving the relative
// ones against root. Nothing is linked until it is given one.
func (self *Output) LinkPathsUnder(root string) *Output {
	self.linkRoot = root
	return self
}

// Status opens a tool-call block. Nothing else may print until it closes.
func (self *Output) Status() *status.Block {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.seal()
	self.separate(false)

	if self.isMidLine {
		self.newline() // before any line owed, so the owed one is empty rather than the end of this
	}

	self.openPendingLine()

	self.measure()

	return status.New(self.drawRow, self.overlay, self.isTerminal, self.columns)
}

// Answer appends one reply delta, separating each run of deltas from other output.
func (self *Output) Answer(delta string) {
	if delta == "" {
		return
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.seal()

	if !self.isStreaming {
		if delta = strings.TrimLeft(delta, "\n"); delta == "" {
			return
		}
	}

	answerText := strings.TrimRight(delta, "\n")
	trailingNewlines := delta[len(answerText):]

	if answerText == "" {
		self.hoardedNewlines += trailingNewlines
		return
	}

	self.separate(true)

	if self.isMidLine && !self.isStreaming {
		self.newline()
	}

	self.write(self.hoardedNewlines + style.Answer(answerText))

	self.hoardedNewlines = trailingNewlines
	self.isStreaming = true
}

// Line writes text on a line of its own after any streamed answer.
func (self *Output) Line(text string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.seal()
	self.separate(false)

	if self.isMidLine {
		self.newline()
	}

	self.write(text)
	self.isStreaming = false
}

// Blank schedules one empty line before the next output. Repeated calls coalesce.
func (self *Output) Blank() {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.isBlankOwed = self.isStacked
}

// End finishes the turn on a complete line. Repeated calls are inert.
func (self *Output) End() {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.seal()

	if self.isMidLine {
		self.newline()
		self.isMidLine = false
		self.hasPendingText = false
	}
}

func (self *Output) overlay(text string, column int) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.at(text)
	self.column = column
}

func (self *Output) drawRow(text string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.separate(false)

	self.write(text)
	self.isStreaming = false
}

func (self *Output) measure() {
	file, ok := self.writer.(*os.File)
	if !ok {
		return
	}

	if columns, lines, err := term.GetSize(int(file.Fd())); err == nil {
		self.columns = columns
		self.lines = lines
	}
}

func (self *Output) newline() {
	self.emit("\n") // not write: a line owed is owed before this one ends, and paid after
}

func (self *Output) write(text string) {
	if text == "" {
		return
	}

	self.openPendingLine()
	self.emit(text)
}

func (self *Output) emit(text string) {
	self.isStacked = true
	fitted := self.fit(text)
	self.advance(text)
	self.count(text)
	self.at(fitted)
}

const apart = 2 // newlines with an empty line between them

func (self *Output) separate(answering bool) {
	if self.isStreaming != answering {
		self.isBlankOwed = self.isStacked
	}

	if !answering {
		self.hoardedNewlines = "" // an answer that ended on blank rows ended before them
	}
}

func (self *Output) openPendingLine() {
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

func (self *Output) advance(text string) {
	if index := strings.LastIndex(text, "\n"); index >= 0 {
		self.isMidLine = false
		text = text[index+1:]
	}

	if text != "" {
		self.isMidLine = true
	}
}

func (self *Output) count(styledText string) {
	text := style.Plain(styledText)
	trailingNewlines := len(text) - len(strings.TrimRight(text, "\n"))

	if trailingNewlines == len(text) {
		self.trailingNewlines += trailingNewlines
		return
	}

	self.trailingNewlines = trailingNewlines
}

func (self *Output) at(text string) {
	if len(self.shownFooter.rows) == 0 {
		self.raw(text)
		return
	}

	self.redraw(text)
}

func (self *Output) linkifyScrollback(text string) string {
	if !self.isTerminal || self.linkRoot == "" {
		return text
	}

	return pathlink.Render(text, self.linkRoot)
}

func (self *Output) raw(text string) {
	if self.nestedUpdates > 0 {
		self.synchronisedBytes.WriteString(text)
		return
	}

	self.writeRaw(text)
}

func (self *Output) writeRaw(text string) {
	_, _ = io.WriteString(self.writer, text)
}
