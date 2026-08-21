package main

import (
	"strconv"
	"strings"
	"testing"
)

type screen struct {
	t           *testing.T
	rows        [][]rune
	row, column int
	columns     int
	isWrapping  bool
}

func visibleScreen(t *testing.T, stream string, columns int) []string {
	t.Helper()

	self := &screen{t: t, columns: columns, isWrapping: true}
	self.play(stream)

	return self.text()
}

func (self *screen) play(stream string) {
	for at := 0; at < len(stream); {
		switch stream[at] {
		case '\x1b':
			at = self.escape(stream, at)
		case '\r':
			self.column = 0
			at++
		case '\n':
			self.row++
			self.column = 0
			at++
		default:
			size := 1
			for size < len(stream)-at && stream[at+size]&0xc0 == 0x80 {
				size++
			}

			self.put([]rune(stream[at : at+size])[0])
			at += size
		}
	}
}

func (self *screen) escape(stream string, at int) int {
	if at+1 >= len(stream) {
		return len(stream)
	}

	switch stream[at+1] {
	case '[':
		return self.control(stream, at)
	case ']':
		return skipUntilStringTerminator(stream, at)
	default:
		self.t.Fatalf("the screen was sent an escape it does not know: %q", stream[at:min(at+8, len(stream))])
		return len(stream)
	}
}

func (self *screen) control(stream string, at int) int {
	end := at + 2
	for end < len(stream) && (stream[end] < 0x40 || stream[end] > 0x7e) {
		end++
	}

	if end >= len(stream) {
		return len(stream)
	}

	parameters := stream[at+2 : end]
	self.apply(stream[end], parameters)

	return end + 1
}

func (self *screen) apply(command byte, parameters string) {
	if strings.HasPrefix(parameters, "?") {
		self.privateMode(command, parameters)
		return
	}

	count := max(1, numberOr(parameters, 1))

	switch command {
	case 'm':
	case 'A':
		self.row = max(0, self.row-count)
	case 'B':
		self.row += count
	case 'C':
		self.column += count
	case 'D':
		self.column = max(0, self.column-count)
	case 'H':
		self.row, self.column = 0, 0
	case 'K':
		self.eraseInRow(numberOr(parameters, 0))
	case 'J':
		self.erase(numberOr(parameters, 0))
	default:
		self.t.Fatalf("the screen was sent a control it does not know: ESC [ %s%c", parameters, command)
	}
}

func (self *screen) privateMode(command byte, parameters string) {
	if command != 'h' && command != 'l' {
		self.t.Fatalf("the screen was sent a private mode it does not know: ESC [ %s%c", parameters, command)
	}

	switch parameters {
	case "?7":
		self.isWrapping = command == 'h'
	case "?25", "?2026":
	default:
		self.t.Fatalf("the screen was sent a private mode it does not know: ESC [ %s%c", parameters, command)
	}
}

func (self *screen) eraseInRow(mode int) {
	if self.row >= len(self.rows) {
		return
	}

	row := self.rows[self.row]

	switch mode {
	case 0:
		if self.column < len(row) {
			self.rows[self.row] = row[:self.column]
		}
	case 1:
		for at := range min(self.column+1, len(row)) {
			row[at] = ' '
		}
	case 2:
		self.rows[self.row] = nil
	}
}

func (self *screen) erase(mode int) {
	switch mode {
	case 0:
		self.eraseInRow(0)

		if self.row+1 < len(self.rows) {
			self.rows = self.rows[:self.row+1]
		}
	case 2, 3:
		self.rows = nil
	default:
		self.t.Fatalf("the screen was asked to erase in a way it does not know: ESC [ %dJ", mode)
	}
}

func (self *screen) put(value rune) {
	if self.column >= self.columns {
		if !self.isWrapping {
			return
		}

		self.row++
		self.column = 0
	}

	for len(self.rows) <= self.row {
		self.rows = append(self.rows, nil)
	}

	row := self.rows[self.row]
	for len(row) <= self.column {
		row = append(row, ' ')
	}

	row[self.column] = value
	self.rows[self.row] = row
	self.column++
}

func (self *screen) text() []string {
	lines := make([]string, 0, len(self.rows))

	for _, row := range self.rows {
		lines = append(lines, strings.TrimRight(string(row), " "))
	}

	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	return lines
}

func skipUntilStringTerminator(stream string, at int) int {
	for end := at + 2; end < len(stream); end++ {
		switch {
		case stream[end] == '\a':
			return end + 1
		case stream[end] == '\x1b' && end+1 < len(stream) && stream[end+1] == '\\':
			return end + 2
		}
	}

	return len(stream)
}

func numberOr(parameters string, fallback int) int {
	if parameters == "" {
		return fallback
	}

	value, err := strconv.Atoi(strings.Split(parameters, ";")[0])
	if err != nil {
		return fallback
	}

	return value
}
