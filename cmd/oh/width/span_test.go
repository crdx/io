package width

import "testing"

// The table is searched, not scanned, so a span out of order or overlapping its neighbour is a
// character silently measured wrong.
func TestTheSpansAreOrderedAndSeparate(t *testing.T) {
	for index := range spans {
		if spans[index].first > spans[index].last {
			t.Errorf("span %d runs backwards: %04X to %04X", index, spans[index].first, spans[index].last)
		}

		if index > 0 && spans[index].first <= spans[index-1].last {
			t.Errorf("span %d starts at %04X, inside the one before it", index, spans[index].first)
		}
	}
}

// An emoji that is drawn as one without being asked to takes two cells, and one that needs a
// variation selector takes one until it gets it.
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
