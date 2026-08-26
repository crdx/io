package output

import (
	"strings"
	"testing"
)

func TestTheConversationWrapsByCellsRatherThanCharacters(t *testing.T) {
	screen := &Screen{writer: &strings.Builder{}, isTTY: true, columns: 5}

	if got := screen.fit("日本語"); got != "日本\r\n語" {
		t.Errorf("expected a break after the second character, got %q", got)
	}

	if screen.column != 2 {
		t.Errorf("expected the cursor two cells along, got %d", screen.column)
	}
}

func TestTextPresentationEmojiDoesNotCreateAPhantomRow(t *testing.T) {
	screen := &Screen{writer: &strings.Builder{}, isTTY: true, columns: 8}

	if got := screen.fit(" test 🖊 \n"); got != " test 🖊 \r\n" {
		t.Errorf("expected the message to remain on one row, got %q", got)
	}

	if screen.openedRows != 1 {
		t.Errorf("expected one opened row, got %d", screen.openedRows)
	}
}

func TestJoinedEmojiAreMeasuredAsOneGrapheme(t *testing.T) {
	screen := &Screen{writer: &strings.Builder{}, isTTY: true, columns: 3}

	if got := screen.fit("👨‍👩‍👧x"); got != "👨‍👩‍👧x" {
		t.Errorf("expected the joined emoji to remain on one row, got %q", got)
	}
}

func TestAnEscapeSequenceTakesNoRoomOnTheRow(t *testing.T) {
	screen := &Screen{writer: &strings.Builder{}, isTTY: true, columns: 5}

	if got := screen.fit("\x1b[31mabcde\x1b[0m"); got != "\x1b[31mabcde\x1b[0m" {
		t.Errorf("expected the colour not to count against the width, got %q", got)
	}
}

func TestACharacterWiderThanTheTerminalStaysWhereItIs(t *testing.T) {
	screen := &Screen{writer: &strings.Builder{}, isTTY: true, columns: 1}

	if got := screen.fit("日"); got != "日" {
		t.Errorf("expected no room to be made for it, got %q", got)
	}

	if screen.column != 1 {
		t.Errorf("expected the column held at the edge, got %d", screen.column)
	}
}
