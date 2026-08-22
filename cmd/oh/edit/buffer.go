package edit

import (
	"unicode"
)

// Buffer is the text being edited and where the cursor sits in it, counted in runes so that a
// character wider than a byte still moves the cursor by one.
type Buffer struct {
	runes  []rune
	cursor int
}

func (self *Buffer) String() string {
	return string(self.runes)
}

// Runes gives the text as the runes it is stored as.
func (self *Buffer) Runes() []rune {
	return self.runes
}

// Cursor gives how many runes lie before the cursor.
func (self *Buffer) Cursor() int {
	return self.cursor
}

// Len gives how many runes the text is, which is not how many bytes it takes.
func (self *Buffer) Len() int {
	return len(self.runes)
}

// Set replaces the text, leaving the cursor at the end of it.
func (self *Buffer) Set(text string) {
	self.runes = []rune(text)
	self.cursor = len(self.runes)
}

// Insert puts text in at the cursor, which follows it.
func (self *Buffer) Insert(text []rune) {
	rest := append(append([]rune(nil), text...), self.runes[self.cursor:]...)
	self.runes = append(self.runes[:self.cursor], rest...)
	self.cursor += len(text)
}

// MoveLeft steps back one rune, stopping at the start of the text.
func (self *Buffer) MoveLeft() {
	if self.cursor > 0 {
		self.cursor--
	}
}

// MoveRight steps on one rune, stopping at the end of the text.
func (self *Buffer) MoveRight() {
	if self.cursor < len(self.runes) {
		self.cursor++
	}
}

// MoveHome goes to the start of the line the cursor is on, not the start of the text.
func (self *Buffer) MoveHome() {
	self.cursor = self.lineStart()
}

// MoveEnd goes to the end of the line the cursor is on, not the end of the text.
func (self *Buffer) MoveEnd() {
	self.cursor = self.lineEnd()
}

// MoveWordLeft goes back over any punctuation and then over the word before it.
func (self *Buffer) MoveWordLeft() {
	self.cursor = self.wordStart()
}

// MoveWordRight goes on over any punctuation and then over the word after it.
func (self *Buffer) MoveWordRight() {
	self.cursor = self.wordEnd()
}

// MoveUp goes to the line above, keeping the column where that line is long enough to hold it, and
// reports whether there was a line above to go to.
func (self *Buffer) MoveUp() bool {
	start := self.lineStart()
	if start == 0 {
		return false
	}

	column := self.cursor - start
	above := self.lineStartAt(start - 1)

	self.cursor = min(above+column, start-1)

	return true
}

// MoveDown goes to the line below, keeping the column where that line is long enough to hold it,
// and reports whether there was a line below to go to.
func (self *Buffer) MoveDown() bool {
	end := self.lineEnd()
	if end == len(self.runes) {
		return false
	}

	column := self.cursor - self.lineStart()
	below := end + 1

	self.cursor = min(below+column, self.lineEndAt(below))

	return true
}

// DeleteBackward takes out the rune before the cursor.
func (self *Buffer) DeleteBackward() {
	if self.cursor > 0 {
		self.remove(self.cursor-1, self.cursor)
	}
}

// DeleteForward takes out the rune after the cursor.
func (self *Buffer) DeleteForward() {
	if self.cursor < len(self.runes) {
		self.remove(self.cursor, self.cursor+1)
	}
}

// DeleteWordBackward takes out everything from the start of the word behind the cursor.
func (self *Buffer) DeleteWordBackward() {
	self.remove(self.wordStart(), self.cursor)
}

func (self *Buffer) remove(start int, end int) {
	if start >= end {
		return
	}

	self.runes = append(self.runes[:start], self.runes[end:]...)
	self.cursor = start
}

func (self *Buffer) lineStart() int {
	return self.lineStartAt(self.cursor)
}

func (self *Buffer) lineStartAt(position int) int {
	for i := position - 1; i >= 0; i-- {
		if self.runes[i] == '\n' {
			return i + 1
		}
	}

	return 0
}

func (self *Buffer) lineEnd() int {
	return self.lineEndAt(self.cursor)
}

func (self *Buffer) lineEndAt(position int) int {
	for i := position; i < len(self.runes); i++ {
		if self.runes[i] == '\n' {
			return i
		}
	}

	return len(self.runes)
}

func (self *Buffer) wordStart() int {
	position := self.cursor

	for position > 0 && !isWord(self.runes[position-1]) {
		position--
	}

	for position > 0 && isWord(self.runes[position-1]) {
		position--
	}

	return position
}

func (self *Buffer) wordEnd() int {
	position := self.cursor

	for position < len(self.runes) && !isWord(self.runes[position]) {
		position++
	}

	for position < len(self.runes) && isWord(self.runes[position]) {
		position++
	}

	return position
}

func isWord(value rune) bool {
	return unicode.IsLetter(value) || unicode.IsDigit(value)
}
