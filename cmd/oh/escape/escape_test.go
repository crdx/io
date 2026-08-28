package escape

import "testing"

func TestGetEndIncludesTheEscapeTerminator(t *testing.T) {
	for _, sequence := range []string{"\x1b[38;2;255;255;255m", "\x1b[K"} {
		runes := []rune(sequence + "after")
		if got := GetEnd(runes, 0); got != len([]rune(sequence)) {
			t.Errorf("GetEnd(%q) = %d, want %d", sequence, got, len([]rune(sequence)))
		}
	}
}

func TestGetEndTakesAnUnterminatedEscapeToTheEnd(t *testing.T) {
	runes := []rune("\x1b[38;2")
	if got := GetEnd(runes, 0); got != len(runes) {
		t.Errorf("GetEnd() = %d, want %d", got, len(runes))
	}
}
