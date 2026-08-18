package width

import (
	"slices"
	"strings"
	"testing"
)

func TestWrappingBreaksAtSpacesAndMidWordWhereThereAreNone(t *testing.T) {
	for _, test := range []struct {
		text  string   // the text to wrap
		cells int      // the width available
		want  []string // the rows expected
	}{
		{"", 8, []string{""}},
		{"hello", 5, []string{"hello"}},
		{"hello!", 5, []string{"hello", "!"}},
		{"one two three", 7, []string{"one two", "three"}},
		{"one two three", 3, []string{"one", "two", "thr", "ee"}},
		{"日本語です", 4, []string{"日本", "語で", "す"}},
		{"a日b", 2, []string{"a", "日", "b"}}, // the wide character straddles the end of the first row
		{"one\ntwo", 8, []string{"one", "two"}},
		{"hello", 0, []string{"hello"}},
	} {
		if got := Wrap(test.text, test.cells); !slices.Equal(got, test.want) {
			t.Errorf("Wrap(%q, %d) = %q, want %q", test.text, test.cells, got, test.want)
		}
	}
}

func TestAStyleThatSpansABreakIsClosedAndOpenedAgain(t *testing.T) {
	got := Wrap("\x1b[1mbold words here\x1b[0m", 10)

	if len(got) != 2 {
		t.Fatalf("expected two rows, got %q", got)
	}

	if !strings.HasPrefix(got[0], "\x1b[1m") || !strings.HasSuffix(got[0], reset) {
		t.Errorf("expected the first row to open and close the style, got %q", got[0])
	}

	if !strings.HasPrefix(got[1], "\x1b[1m") {
		t.Errorf("expected the second row to open the style again, got %q", got[1])
	}
}

func TestNoRowIsWiderThanItWasAskedFor(t *testing.T) {
	text := "\x1b[1mA reasonably long\x1b[0m sentence with 日本語 in it and averylongidentifier too"

	for cells := 2; cells <= 40; cells++ {
		for _, row := range Wrap(text, cells) {
			if got := Of(plain(row)); got > cells {
				t.Errorf("Wrap(_, %d) gave a row of %d cells: %q", cells, got, row)
			}
		}
	}
}

func plain(text string) string {
	var out strings.Builder

	for _, one := range split(text) {
		if !one.escape {
			out.WriteString(one.text)
		}
	}

	return out.String()
}
