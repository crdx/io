package line

import (
	"strings"
	"testing"

	"crdx.org/io/cmd/oh/width"
)

var layouts = []string{
	"the quick brown fox jumps over the lazy dog",
	"a bb ccc dddd eeeee ffffff",
	"one\ntwo three four\nfive",
	"  indented  gaps   here",
	"日本語 の テキスト です",
	"\n\nabc\n\n",
}

// Every prefix of every text, at every room and with the cursor at every position in it, has to
// come out drawable: no row wider than the room it was given, no cursor off the rows it belongs
// to, and no row led by the whitespace a break should have eaten.
func TestEveryLayoutIsDrawable(t *testing.T) {
	forEachLayout(t, func(t *testing.T, rows []string, cursorRow int, cursorColumn int, room int) {
		t.Helper()

		for index, row := range rows {
			if index > 0 && strings.HasPrefix(row, " ") && strings.TrimSpace(row) != "" {
				t.Errorf("row %d is led by whitespace a break should have eaten: %q", index, rows)
			}

			if width.Of(row) > room && len([]rune(strings.TrimRight(row, " "))) > 1 {
				t.Errorf("row %d overruns %d cells: %q", index, room, rows)
			}
		}

		if cursorRow < 0 || cursorRow >= len(rows) {
			t.Errorf("cursor row %d is off the %d rows: %q", cursorRow, len(rows), rows)
		}

		if cursorColumn < 0 || cursorColumn > room {
			t.Errorf("cursor column %d is off the %d cells: %q", cursorColumn, room, rows)
		}
	})
}

// Walking the cursor forward through the text has to walk it forward through the rows as well, so
// that no keypress moving right ever draws it further left.
func TestTheCursorNeverGoesBackwards(t *testing.T) {
	for _, text := range layouts {
		runes := []rune(text)

		for room := 1; room <= 14; room++ {
			lastRow, lastColumn := 0, 0

			for cursor := range len(runes) + 1 {
				_, row, column := layout(&buffer{runes: runes, cursor: cursor}, room)

				if row < lastRow || (row == lastRow && column < lastColumn) {
					t.Fatalf(
						"%q at %d in room %d went from %d,%d back to %d,%d",
						text, cursor, room, lastRow, lastColumn, row, column,
					)
				}

				lastRow, lastColumn = row, column
			}
		}
	}
}

func forEachLayout(t *testing.T, check func(*testing.T, []string, int, int, int)) {
	t.Helper()

	for _, text := range layouts {
		runes := []rune(text)

		for room := 1; room <= 14; room++ {
			for length := range len(runes) + 1 {
				for cursor := range length + 1 {
					rows, row, column := layout(&buffer{runes: runes[:length], cursor: cursor}, room)
					check(t, rows, row, column, room)

					if t.Failed() {
						return
					}
				}
			}
		}
	}
}
