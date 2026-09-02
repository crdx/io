package output

import (
	"strings"
	"testing"
)

func footerRows(count int) []string {
	rows := make([]string, count)
	for i := range rows {
		rows[i] = "row"
	}
	return rows
}

func TestAFooterShorterThanTheTerminalIsLeftAlone(t *testing.T) {
	screen := NewTerminalOfSize(&strings.Builder{}, 40, 10)

	rows, cursorRow := screen.fitFooter(footerRows(4), 3)

	if len(rows) != 4 || cursorRow != 3 {
		t.Errorf("kept %d rows focused at %d, want 4 at 3", len(rows), cursorRow)
	}
}

func TestAFooterTallerThanTheTerminalIsCutToFit(t *testing.T) {
	screen := NewTerminalOfSize(&strings.Builder{}, 40, 10)

	rows, cursorRow := screen.fitFooter(footerRows(40), 39)

	if len(rows) != 10 {
		t.Fatalf("kept %d rows, want the terminal's 10", len(rows))
	}
	if cursorRow != 9 {
		t.Errorf("focused at %d, want the last row", cursorRow)
	}
	if !strings.Contains(rows[0], "31 more lines") {
		t.Errorf("first row is %q, want it to say what was hidden", rows[0])
	}
}

func TestAFooterCutAtBothEndsSaysSoAtBoth(t *testing.T) {
	screen := NewTerminalOfSize(&strings.Builder{}, 40, 10)

	rows, cursorRow := screen.fitFooter(footerRows(40), 20)

	if len(rows) != 10 {
		t.Fatalf("kept %d rows, want the terminal's 10", len(rows))
	}
	if !strings.Contains(rows[0], "more lines") || !strings.Contains(rows[len(rows)-1], "more lines") {
		t.Errorf("kept %q, want a notice at each end", rows)
	}
	if cursorRow <= 0 || cursorRow >= len(rows)-1 {
		t.Errorf("focused at %d, want it between the two notices", cursorRow)
	}
}

func TestOneHiddenLineIsSaidInTheSingular(t *testing.T) {
	screen := NewTerminalOfSize(&strings.Builder{}, 40, 10)

	rows, _ := screen.fitFooter(footerRows(40), 38)

	last := rows[len(rows)-1]
	if !strings.Contains(last, "1 more line") || strings.Contains(last, "lines") {
		t.Errorf("last row is %q, want one line said in the singular", last)
	}
}

func TestAOneRowOverflowHidesTwoLinesBecauseTheNoticeCostsOne(t *testing.T) {
	screen := NewTerminalOfSize(&strings.Builder{}, 40, 10)

	rows, _ := screen.fitFooter(footerRows(11), 10)

	if !strings.Contains(rows[0], "2 more lines") {
		t.Errorf("first row is %q, want the notice to count itself", rows[0])
	}
}

func TestAFooterIsLeftAloneWhenTheHeightIsUnknown(t *testing.T) {
	screen := New(&strings.Builder{})

	rows, cursorRow := screen.fitFooter(footerRows(40), 39)

	if len(rows) != 40 || cursorRow != 39 {
		t.Errorf("kept %d rows focused at %d, want all 40 at 39", len(rows), cursorRow)
	}
}

func TestATerminalTooShortForANoticeStillFits(t *testing.T) {
	for _, lines := range []int{1, 2} {
		screen := NewTerminalOfSize(&strings.Builder{}, 40, lines)

		rows, cursorRow := screen.fitFooter(footerRows(40), 39)

		if len(rows) != lines {
			t.Errorf("height %d kept %d rows, want %d", lines, len(rows), lines)
		}
		if cursorRow < 0 || cursorRow >= len(rows) {
			t.Errorf("height %d focused at %d, outside the %d rows kept", lines, cursorRow, len(rows))
		}
	}
}
