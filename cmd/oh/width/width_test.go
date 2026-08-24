package width

import (
	"reflect"
	"testing"
)

func TestWhatEachKindOfCharacterTakes(t *testing.T) {
	for text, want := range map[string]int{
		"":        0,
		"hello":   5,
		"日本":      4,
		"한국":      4,
		"ｆｕｌｌ":    8,
		"a日b":     4,
		"🙂":       2,
		"e\u0301": 1,
		"\u200d":  0,
		"❤️":      2,
		"1️⃣":     2,
		"🇬🇧":      2,
		"👩‍🚀":     2,
		"🏳️‍🌈":    2,
		"👨‍👩‍👧‍👦": 2,
	} {
		if got := Of(text); got != want {
			t.Errorf("Of(%q) = %d, want %d", text, got, want)
		}
	}
}

func TestCellsKeepGraphemeClustersTogether(t *testing.T) {
	got := Cells("a👩‍🚀❤️1️⃣b")
	want := []string{"a", "👩‍🚀", "", "❤️", "", "1️⃣", "", "b"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func TestTheTableIsInOrderAndDoesNotOverlap(t *testing.T) {
	for i, one := range spans {
		if one.first > one.last {
			t.Errorf("span %d runs backwards: %#x to %#x", i, one.first, one.last)
		}

		if i > 0 && one.first <= spans[i-1].last {
			t.Errorf("span %d starts at %#x, inside the one before it", i, one.first)
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
		{"日本語", 3, "日", 2},
		{"日本語", 1, "", 0},
		{"hello", 3, "hel", 3},
		{"hello", 9, "hello", 5},
		{"", 4, "", 0},
		{"👩‍🚀x", 2, "👩‍🚀", 2},
		{"🏳️‍🌈x", 2, "🏳️‍🌈", 2},
		{"👨‍👩‍👧‍👦x", 2, "👨‍👩‍👧‍👦", 2},
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
