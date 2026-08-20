package width

import "testing"

func TestWhatEachKindOfCharacterTakes(t *testing.T) {
	for text, want := range map[string]int{
		"":        0,
		"hello":   5,
		"日本":      4,
		"한국":      4,
		"ｆｕｌｌ":    8,
		"a日b":     4,
		"🙂":       2,
		"e\u0301": 1, // e with an acute accent hung off it
		"\u200d":  0, // a joiner, which is drawn as nothing
	} {
		if got := Of(text); got != want {
			t.Errorf("Of(%q) = %d, want %d", text, got, want)
		}
	}
}

func TestTheTableIsInOrderAndDoesNotOverlap(t *testing.T) {
	for index, one := range spans {
		if one.first > one.last {
			t.Errorf("span %d runs backwards: %#x to %#x", index, one.first, one.last)
		}

		if index > 0 && one.first <= spans[index-1].last {
			t.Errorf("span %d starts at %#x, inside the one before it", index, one.first)
		}
	}
}

func TestCutStopsShortOfACharacterThatWouldNotFit(t *testing.T) {
	for _, test := range []struct {
		text  string
		cells int
		want  string
		took  int
	}{
		{"日本語", 4, "日本", 4},
		{"日本語", 3, "日", 2}, // the second character would straddle the end
		{"日本語", 1, "", 0},  // and so would the first
		{"hello", 3, "hel", 3},
		{"hello", 9, "hello", 5},
		{"", 4, "", 0},
	} {
		got, took := Cut(test.text, test.cells)

		if got != test.want || took != test.took {
			t.Errorf(
				"Cut(%q, %d) = %q, %d, want %q, %d",
				test.text, test.cells, got, took, test.want, test.took,
			)
		}
	}
}
