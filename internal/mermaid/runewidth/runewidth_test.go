package runewidth

import "testing"

func TestWidthsUseTerminalCells(t *testing.T) {
	if got := StringWidth("a界"); got != 3 {
		t.Errorf("got string width %d, want 3", got)
	}
	if got := RuneWidth('界'); got != 2 {
		t.Errorf("got rune width %d, want 2", got)
	}
}
