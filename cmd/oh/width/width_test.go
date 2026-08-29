package width

import (
	"reflect"
	"strings"
	"testing"

	"github.com/rivo/uniseg"
)

func TestWhatEachKindOfCharacterTakes(t *testing.T) {
	for text, want := range map[string]int{
		"":                                0,
		"hello":                           5,
		"日本":                              4,
		"한국":                              4,
		"ｆｕｌｌ":                            8,
		"a日b":                             4,
		"🙂":                               2,
		"e\u0301":                         1,
		"\u200d":                          0,
		"❤️":                              2,
		"1️⃣":                             2,
		"🇬🇧":                              2,
		"👩‍🚀":                             2,
		"🏳️‍🌈":                            2,
		"👨‍👩‍👧‍👦":                         2,
		"\x1b[31mred\x1b[0m":              3,
		"\x1b[4C":                         4,
		"\x1b]66;s=2:n=3:d=4:w=2;🐟\x1b\\": 4,
	} {
		if got := Of(text); got != want {
			t.Errorf("Of(%q) = %d, want %d", text, got, want)
		}
	}
}

func TestClustersSurviveTheBoundaryBetweenPlainAndComplexText(t *testing.T) {
	for text, want := range map[string][]string{
		"a🇬🇧b":     {"a", "🇬🇧", "b"},
		"🇬🇧🇬🇧":     {"🇬🇧", "🇬🇧"},
		"🇬🇧🇫🇷🇩🇪":   {"🇬🇧", "🇫🇷", "🇩🇪"},
		"a\r\nb":   {"a", "\r\n", "b"},
		"e\u0301x": {"e\u0301", "x"},
		"1️⃣2️⃣":   {"1️⃣", "2️⃣"},
		"👨‍👩‍👧‍👦👨‍👩‍👧‍👦": {"👨‍👩‍👧‍👦", "👨‍👩‍👧‍👦"},
		"日本ｆ": {"日", "本", "ｆ"},
	} {
		var got []string
		for grapheme := range Graphemes(text) {
			got = append(got, grapheme)
		}

		if !reflect.DeepEqual(got, want) {
			t.Errorf("Graphemes(%q) = %q, want %q", text, got, want)
		}
	}
}

func FuzzGraphemesMatchTheSegmenter(fuzzer *testing.F) {
	for _, text := range []string{
		"hello",
		"a🇬🇧b",
		"🇬🇧🇬🇧",
		"a\r\nb",
		"e\u0301x",
		"1️⃣2️⃣",
		"👨‍👩‍👧‍👦",
		"日本ｆ",
		"\x1b[31mred\x1b[0m",
	} {
		fuzzer.Add(text)
	}

	fuzzer.Fuzz(func(t *testing.T, text string) {
		wantClusters, wantCells := segmented(text)

		var gotClusters []string
		var gotCells []int
		for grapheme, cells := range Graphemes(text) {
			gotClusters = append(gotClusters, grapheme)
			gotCells = append(gotCells, cells)
		}

		if !reflect.DeepEqual(gotClusters, wantClusters) {
			t.Fatalf("Graphemes(%q) = %q, want %q", text, gotClusters, wantClusters)
		}
		if !reflect.DeepEqual(gotCells, wantCells) {
			t.Fatalf("Graphemes(%q) took %v cells, want %v", text, gotCells, wantCells)
		}

		if taken, took := Cut(text, len(text)*2); taken != text || took != sum(wantCells) {
			t.Fatalf("Cut(%q, all) = %q, %d, want the whole of it and %d", text, taken, took, sum(wantCells))
		}

		if !strings.ContainsRune(text, '\x1b') {
			if got := Of(text); got != sum(wantCells) {
				t.Fatalf("Of(%q) = %d, want %d", text, got, sum(wantCells))
			}
		}
	})
}

func segmented(text string) ([]string, []int) {
	var clusters []string
	var cells []int

	graphemes := uniseg.NewGraphemes(text)
	for graphemes.Next() {
		clusters = append(clusters, graphemes.Str())
		cells = append(cells, graphemeWidth(graphemes.Str(), graphemes.Width()))
	}

	return clusters, cells
}

func sum(values []int) int {
	total := 0
	for _, value := range values {
		total += value
	}

	return total
}

func TestCellsKeepGraphemeClustersTogether(t *testing.T) {
	got := Cells("a👩‍🚀❤️1️⃣b")
	want := []string{"a", "👩‍🚀", "", "❤️", "", "1️⃣", "", "b"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
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
