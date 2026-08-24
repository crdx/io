package color

import "testing"

func TestColourLeavesTerminalTextUnchanged(t *testing.T) {
	colour := HEX("#fff")
	if got := colour.Sprint(42); got != "42" {
		t.Errorf("got %q, want 42", got)
	}
}
