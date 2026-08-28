package strutil_test

import (
	"reflect"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"crdx.org/io/internal/util/strutil"
)

func TestCapitaliseUppercasesTheFirstRune(t *testing.T) {
	for text, want := range map[string]string{
		"":        "",
		"already": "Already",
		"éclair":  "Éclair",
	} {
		if got := strutil.Capitalise(text); got != want {
			t.Errorf("Capitalise(%q) = %q, want %q", text, got, want)
		}
	}
}

func TestLinesDoesNotAddALineAfterATrailingNewline(t *testing.T) {
	got := strutil.Lines("one\ntwo\n")
	want := []string{"one", "two"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func TestLinesKeepsEmptyLines(t *testing.T) {
	got := strutil.Lines("one\n\ntwo")
	want := []string{"one", "", "two"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func TestFlattenPutsTextOnOneLine(t *testing.T) {
	for name, test := range map[string]struct {
		text string
		want string
	}{
		"lines":               {text: "one\ntwo\n", want: "one two"},
		"runs of whitespace":  {text: "  one \t\t two  ", want: "one two"},
		"carriage returns":    {text: "one\r\n\r\ntwo", want: "one two"},
		"colour":              {text: "\x1b[32mok\x1b[0m all six steps", want: "ok all six steps"},
		"a two-byte sequence": {text: "one\x1bctwo", want: "onetwo"},
		"a trailing escape":   {text: "one\x1b", want: "one"},
		"cursor movement":     {text: "one\x1b[2J\x1b[Htwo", want: "onetwo"},
		"a window title":      {text: "\x1b]0;building\x07one", want: "one"},
		"a title ended by st": {text: "\x1b]0;building\x1b\\one", want: "one"},
		"a backspace":         {text: "one\btwo", want: "onetwo"},
		"a bell":              {text: "done\a", want: "done"},
		"nothing":             {text: "", want: ""},
		"only whitespace":     {text: " \n\t ", want: ""},
	} {
		t.Run(name, func(t *testing.T) {
			if got := strutil.Flatten(test.text); got != test.want {
				t.Errorf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestPrintableLeavesEveryOtherByteWhereItWas(t *testing.T) {
	for name, test := range map[string]struct {
		text string
		want string
	}{
		"colour":          {text: "\x1b[32mok", want: " [32mok"},
		"cursor movement": {text: "one\x1b[2Jtwo", want: "one [2Jtwo"},
		"lines":           {text: "cat <<EOF\none\nEOF", want: "cat <<EOF one EOF"},
		"tabs":            {text: "one\ttwo", want: "one two"},
		"a bell":          {text: "done\a", want: "done "},
		"ordinary text":   {text: "grep -r 'func New' .", want: "grep -r 'func New' ."},
		"nothing":         {text: "", want: ""},
	} {
		t.Run(name, func(t *testing.T) {
			got := strutil.Printable(test.text)

			if got != test.want {
				t.Errorf("got %q, want %q", got, test.want)
			}

			if len(got) != len(test.text) {
				t.Errorf("expected %d bytes to stay %d, got %d", len(test.text), len(test.text), len(got))
			}
		})
	}
}

func FuzzPrintable(f *testing.F) {
	for _, seed := range []string{
		"grep -r 'func New' .",
		"\x1b[32mok\x1b[0m",
		"\x1b]0;building\x07",
		"cat <<EOF\none\nEOF",
		"\a\b\v\x00\x7f",
		"\xff\xfe",
		"",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, text string) {
		printable := strutil.Printable(text)

		for _, character := range printable {
			if unicode.IsControl(character) {
				t.Fatalf("expected nothing a terminal would act on, got %q in %q", character, printable)
			}
		}

		if utf8.ValidString(text) && len(printable) != len(text) {
			t.Errorf("expected %d bytes back, got %d in %q", len(text), len(printable), printable)
		}
	})
}

func FuzzFlatten(f *testing.F) {
	for _, seed := range []string{
		"one\ntwo\n",
		"\x1b[32mok\x1b[0m all six steps",
		"\x1b[2J\x1b[H",
		"\x1b]0;building\x07",
		"\x1b]0;building\x1b\\",
		"\x1b]0;never terminated",
		"\x1b[38;2;150;152;150",
		"\x1b",
		"\x1b[",
		"\x1b]",
		"\x1bc",
		"\a\b\v\x00\x7f",
		"\xff\xfe",
		"  \t\r\n  ",
		"",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, text string) {
		flattened := strutil.Flatten(text)

		for _, character := range flattened {
			if unicode.IsControl(character) {
				t.Fatalf("expected nothing a terminal would act on, got %q in %q", character, flattened)
			}
		}

		if !utf8.ValidString(flattened) {
			t.Errorf("expected text a terminal can measure, got %q", flattened)
		}

		if tidied := strings.Join(strings.Fields(flattened), " "); tidied != flattened {
			t.Errorf("expected single spaces between words and none at either end, got %q", flattened)
		}

		if again := strutil.Flatten(flattened); again != flattened {
			t.Errorf("expected flattening to settle, got %q and then %q", flattened, again)
		}
	})
}

func TestNoTextHasNoLines(t *testing.T) {
	if got := strutil.Lines(""); got != nil {
		t.Errorf("got %#v, want nil", got)
	}
}
