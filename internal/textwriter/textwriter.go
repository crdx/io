package textwriter

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

type Writer struct {
	builder     strings.Builder
	isSpaceHeld bool
}

func (self *Writer) String() string {
	return self.builder.String()
}

func (self *Writer) Text(value string) {
	words := strings.Fields(value)
	if len(words) == 0 {
		self.isSpaceHeld = self.isSpaceHeld || value != ""

		return
	}

	first, _ := utf8.DecodeRuneInString(value)
	last, _ := utf8.DecodeLastRuneInString(value)

	if (self.isSpaceHeld || unicode.IsSpace(first)) && self.wantsSpace() {
		_ = self.builder.WriteByte(' ')
	}

	_, _ = self.builder.WriteString(strings.Join(words, " "))
	self.isSpaceHeld = unicode.IsSpace(last)
}

func (self *Writer) Raw(value string) {
	if value == "" {
		return
	}

	if self.isSpaceHeld && self.wantsSpace() {
		_ = self.builder.WriteByte(' ')
	}

	self.isSpaceHeld = false
	_, _ = self.builder.WriteString(value)
}

func (self *Writer) Newlines(count int) {
	self.isSpaceHeld = false
	contents := self.String()

	written := 0
	for i := len(contents) - 1; i >= 0 && contents[i] == '\n'; i-- {
		written++
	}

	if missing := count - written; missing > 0 {
		_, _ = self.builder.WriteString(strings.Repeat("\n", missing))
	}
}

func (self *Writer) wantsSpace() bool {
	contents := self.String()
	if contents == "" {
		return false
	}

	last := contents[len(contents)-1]

	return last != ' ' && last != '\n'
}
