package width

import "testing"

func TestTheSpansAreOrderedAndSeparate(t *testing.T) {
	for i := range spans {
		if spans[i].first > spans[i].last {
			t.Errorf("span %d runs backwards: %04X to %04X", i, spans[i].first, spans[i].last)
		}

		if i > 0 && spans[i].first <= spans[i-1].last {
			t.Errorf("span %d starts at %04X, inside the one before it", i, spans[i].first)
		}
	}
}

func TestTheEmojiDrawnWithoutAskingTakeTwoCells(t *testing.T) {
	for value, want := range map[rune]int{
		'⭐': 2,
		'✅': 2,
		'❌': 2,
		'🚀': 2,
		'🟡': 2,
		'🔴': 2,
		'⚠': 1,
		'✂': 1,
		'a': 1,
		'日': 2,
	} {
		if got := Rune(value); got != want {
			t.Errorf("Rune(%q) = %d, want %d", value, got, want)
		}
	}
}
