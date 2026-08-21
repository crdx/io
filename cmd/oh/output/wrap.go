package output

import (
	"strings"

	"crdx.org/io/cmd/oh/width"
)

const (
	noAutoWrap = "\x1b[?7l"
	autoWrap   = "\x1b[?7h"
)

func (self *Screen) fit(text string) string {
	if self.columns <= 0 {
		self.openedRows += strings.Count(text, "\n")
		return text
	}

	var out strings.Builder

	escaped := false
	last := rune(0)

	for _, value := range text {
		switch {
		case escaped:
			escaped = value != 'm' && value != 'K'

			out.WriteRune(value)

			continue

		case value == '\x1b':
			escaped = true

			out.WriteRune(value)

			continue

		case value == '\n':
			if last != '\r' {
				out.WriteRune('\r')
			}

			self.column = 0
			self.openedRows++

		case value == '\r':
			self.column = 0

		default:
			cells := width.Rune(value)

			if self.column+cells > self.columns && self.column > 0 {
				out.WriteString("\r\n")

				self.column = 0
				self.openedRows++
			}

			self.column = min(self.column+cells, self.columns)
		}

		out.WriteRune(value)

		last = value
	}

	return out.String()
}
