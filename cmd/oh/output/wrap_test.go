package output

import (
	"strings"
	"testing"
)

// The terminal is told not to wrap, so the conversation is broken up here. A two-cell character
// counted as one would put a cell more on the row than there is room for, and the terminal would
// drop it at the margin.
func TestTheConversationWrapsByCellsRatherThanCharacters(t *testing.T) {
	screen := &Output{writer: &strings.Builder{}, terminal: true, columns: 5}

	if got := screen.fit("日本語"); got != "日本\r\n語" {
		t.Errorf("expected a break after the second character, got %q", got)
	}

	if screen.column != 2 {
		t.Errorf("expected the cursor two cells along, got %d", screen.column)
	}
}

func TestAnEscapeSequenceTakesNoRoomOnTheRow(t *testing.T) {
	screen := &Output{writer: &strings.Builder{}, terminal: true, columns: 5}

	if got := screen.fit("\x1b[31mabcde\x1b[0m"); got != "\x1b[31mabcde\x1b[0m" {
		t.Errorf("expected the colour not to count against the width, got %q", got)
	}
}

// A character wider than the terminal cannot be made to fit, so opening a row for it only wastes
// the row. What matters is that the column never claims to be past the edge, because everything
// that comes after is measured against the room left on the line.
func TestACharacterWiderThanTheTerminalStaysWhereItIs(t *testing.T) {
	screen := &Output{writer: &strings.Builder{}, terminal: true, columns: 1}

	if got := screen.fit("日"); got != "日" {
		t.Errorf("expected no room to be made for it, got %q", got)
	}

	if screen.column != 1 {
		t.Errorf("expected the column held at the edge, got %d", screen.column)
	}
}
