package line

import (
	"unicode"
)

type buffer struct {
	runes  []rune
	cursor int
}

func (self *buffer) String() string {
	return string(self.runes)
}

func (self *buffer) Runes() []rune {
	return self.runes
}

func (self *buffer) Cursor() int {
	return self.cursor
}

func (self *buffer) Len() int {
	return len(self.runes)
}

func (self *buffer) Set(text string) {
	self.runes = []rune(text)
	self.cursor = len(self.runes)
}

func (self *buffer) Insert(text []rune) {
	rest := append(append([]rune(nil), text...), self.runes[self.cursor:]...)
	self.runes = append(self.runes[:self.cursor], rest...)
	self.cursor += len(text)
}

func (self *buffer) MoveLeft() {
	if self.cursor > 0 {
		self.cursor--
	}
}

func (self *buffer) MoveRight() {
	if self.cursor < len(self.runes) {
		self.cursor++
	}
}

func (self *buffer) MoveHome() {
	self.cursor = self.lineStart()
}

func (self *buffer) MoveEnd() {
	self.cursor = self.lineEnd()
}

func (self *buffer) MoveWordLeft() {
	self.cursor = self.wordStart()
}

func (self *buffer) MoveWordRight() {
	self.cursor = self.wordEnd()
}

func (self *buffer) MoveUp() bool {
	start := self.lineStart()
	if start == 0 {
		return false
	}

	column := self.cursor - start
	above := self.lineStartAt(start - 1)

	self.cursor = min(above+column, start-1)

	return true
}

func (self *buffer) MoveDown() bool {
	end := self.lineEnd()
	if end == len(self.runes) {
		return false
	}

	column := self.cursor - self.lineStart()
	below := end + 1

	self.cursor = min(below+column, self.lineEndAt(below))

	return true
}

func (self *buffer) DeleteBackward() {
	if self.cursor > 0 {
		self.remove(self.cursor-1, self.cursor)
	}
}

func (self *buffer) DeleteForward() {
	if self.cursor < len(self.runes) {
		self.remove(self.cursor, self.cursor+1)
	}
}

func (self *buffer) DeleteWordBackward() {
	self.remove(self.wordStart(), self.cursor)
}

func (self *buffer) remove(start int, end int) {
	if start >= end {
		return
	}

	self.runes = append(self.runes[:start], self.runes[end:]...)
	self.cursor = start
}

func (self *buffer) lineStart() int {
	return self.lineStartAt(self.cursor)
}

func (self *buffer) lineStartAt(position int) int {
	for i := position - 1; i >= 0; i-- {
		if self.runes[i] == '\n' {
			return i + 1
		}
	}

	return 0
}

func (self *buffer) lineEnd() int {
	return self.lineEndAt(self.cursor)
}

func (self *buffer) lineEndAt(position int) int {
	for i := position; i < len(self.runes); i++ {
		if self.runes[i] == '\n' {
			return i
		}
	}

	return len(self.runes)
}

func (self *buffer) wordStart() int {
	position := self.cursor

	for position > 0 && !isWord(self.runes[position-1]) {
		position--
	}

	for position > 0 && isWord(self.runes[position-1]) {
		position--
	}

	return position
}

func (self *buffer) wordEnd() int {
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
