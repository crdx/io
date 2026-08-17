package markdown

import (
	"slices"
	"strings"
	"testing"

	"crdx.org/io/cmd/oh/theme"
)

const answer = `# Findings

The **first** thing is that ` + "`read`" + ` is *cheap*, and the second is not.

` + "```go" + `
func main() {
	fmt.Println("hello")
}
` + "```" + `

- one
- two
    - nested

| Name | What it does  |  Cost |
|:-----|:-------------:|------:|
| read | takes a file  |     1 |
| bash | runs a thing  |    40 |

> and that is that
`

const columns = 80

// The renderer holds nothing between calls, so an answer arriving a byte at a time is drawn from
// what it says so far and nothing else. Only the last drawing can differ from the whole.
func TestAnAnswerDrawnADeltaAtATimeIsTheAnswerDrawnAtOnce(t *testing.T) {
	var last []string

	for length := range len(answer) + 1 {
		last = Render(answer[:length], columns)

		for _, row := range last {
			if got := theme.Width(row); got > columns {
				t.Fatalf("a prefix of %d gave a row of %d cells: %q", length, got, row)
			}
		}
	}

	if want := Render(answer, columns); !slices.Equal(last, want) {
		t.Errorf("the last drawing was %q, want %q", last, want)
	}
}

// Every frame works out the columns from every row parsed so far, so a table widens as its rows
// arrive rather than being frozen by whatever was known when its first row came in.
func TestATableWidensAsItsRowsArrive(t *testing.T) {
	short := Render("| a | b |\n|---|---|\n| x | y |\n", columns)
	long := Render("| a | b |\n|---|---|\n| x | y |\n| a much wider cell | y |\n", columns)

	if theme.Width(long[0]) <= theme.Width(short[0]) {
		t.Errorf("expected the table to widen, got %q and %q", short[0], long[0])
	}

	for _, row := range long {
		if theme.Width(row) != theme.Width(long[0]) {
			t.Errorf("expected every row the same width, got %q against %q", row, long[0])
		}
	}
}

// Too narrow for one cell per column, a table is abandoned and said as its rows of text, which is
// at least readable where broken borders are not.
func TestATableTooNarrowForItsColumnsIsAbandoned(t *testing.T) {
	source := "| a | b |\n|---|---|\n| x | y |"

	got := theme.Plain(strings.Join(Render(source, 8), " "))

	for _, want := range []string{"a", "b", "x", "y"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q to survive, got %q", want, got)
		}
	}

	if strings.ContainsAny(got, "┌│└") {
		t.Errorf("expected no borders to be drawn, got %q", got)
	}
}

func TestAnUnclosedFenceIsStillCode(t *testing.T) {
	got := Render("```go\nfunc main() {\n", columns)

	if len(got) != 1 || !strings.Contains(theme.Plain(got[0]), "func main() {") {
		t.Errorf("expected the open block to be drawn as code, got %q", got)
	}

	if !strings.HasPrefix(theme.Plain(got[0]), "  ") {
		t.Errorf("expected the code to be indented, got %q", got[0])
	}
}

// A delimiter is only a delimiter once something closes it, so an answer cut off partway through a
// bold run never shows its punctuation and then takes it away again.
func TestAnUnclosedDelimiterIsLiteral(t *testing.T) {
	for text, want := range map[string]string{
		"**bold":         "**bold",
		"**bold**":       "bold",
		"a `code` span":  "a code span",
		"`**not bold**`": "**not bold**",
		"~~struck~~":     "struck",
		"[text](url)":    "text (url)",
		"\\*starred\\*":  "*starred*",
	} {
		got := theme.Plain(strings.Join(Render(text, columns), "\n"))

		if got != want {
			t.Errorf("Render(%q) drew %q, want %q", text, got, want)
		}
	}
}

func TestANestedListKeepsItsChildrenWithTheirParent(t *testing.T) {
	source := "- `one`\n\n  - child\n  - another\n- `two`\n\n  - child"
	got := theme.Plain(strings.Join(Render(source, columns), "\n"))
	want := "• one\n  • child\n  • another\n• two\n  • child"

	if got != want {
		t.Errorf("nested list drew %q, want %q", got, want)
	}
}

func TestTheBlocksAreDrawnWithoutTheirPunctuation(t *testing.T) {
	got := theme.Plain(strings.Join(Render(answer, columns), "\n"))

	for _, gone := range []string{"#", "```", "**", "- one", "|"} {
		if strings.Contains(got, gone) {
			t.Errorf("expected %q to be drawn rather than shown, got %q", gone, got)
		}
	}

	for _, want := range []string{"Findings", "• one", "│ and that is that", "┌", "read"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q to be drawn, got %q", want, got)
		}
	}
}

// A line opening on the fence characters with anything after them is content. Closing on it drops
// the rest of the answer, which is the worst thing a renderer can do to one.
func TestAFenceIsClosedOnlyByALineThatIsAFence(t *testing.T) {
	got := theme.Plain(strings.Join(Render("```\none\n```go still open\ntwo\n```\nafter", columns), "\n"))

	for _, want := range []string{"one", "```go still open", "two", "after"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q to survive, got %q", want, got)
		}
	}
}

// A style inside another is off from its own reset onwards unless the one outside it is opened
// again, so a bold run with anything styled inside it stops being bold partway through.
func TestAStyleInsideAnotherKeepsTheOneOutsideIt(t *testing.T) {
	got := Render("**one `two` three**", columns)[0]

	if strings.Count(got, "\x1b[1m") < 2 {
		t.Errorf("expected the bold to be opened again after the inner style, got %q", got)
	}
}

func TestATableHeadingIsDrawnAsMarkdownRatherThanShown(t *testing.T) {
	got := theme.Plain(strings.Join(Render("| **Name** | `Kind` |\n|---|---|\n| a | b |", columns), "\n"))

	if strings.Contains(got, "**") || strings.Contains(got, "`") {
		t.Errorf("expected the heading punctuation to be drawn, got %q", got)
	}
}

// Highlighting is chroma's reading of the code and the theme's palette, and nothing of chroma's
// own: a language it has no lexer for is drawn in the one colour code is drawn in.
func TestCodeIsHighlightedWhereTheLanguageIsKnown(t *testing.T) {
	known := Render("```go\nfunc main() {}\n```", columns)
	unknown := Render("```nosuchlanguage\nfunc main() {}\n```", columns)

	if theme.Plain(known[0]) != theme.Plain(unknown[0]) {
		t.Errorf("expected the same code either way, got %q and %q", known[0], unknown[0])
	}

	if known[0] == unknown[0] {
		t.Errorf("expected the known language to be highlighted, got %q", known[0])
	}

	if strings.Count(unknown[0], "\x1b[") != 2 {
		t.Errorf("expected the unknown language to be drawn as one run, got %q", unknown[0])
	}
}

func TestOnlyTheBashExecutableIsHighlighted(t *testing.T) {
	got := Highlight("go test ./cmd/oh", "bash")
	want := theme.Function("go") + theme.Block(" test ./cmd/oh")

	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// A code block indented past the width has nothing to indent it by, and a row wider than the
// terminal is drawn over the edge of it.
func TestNoBlockOverrunsANarrowTerminal(t *testing.T) {
	blocks := []string{
		"```\nabc\n```", "> quoted", "# heading", "- item", "| a | b |\n|---|---|\n| x | y |",
		"a paragraph of several words", "---",
	}

	for _, block := range blocks {
		for cells := 1; cells <= 12; cells++ {
			for _, row := range Render(block, cells) {
				if got := theme.Width(row); got > cells {
					t.Errorf("Render(%q, %d) gave a row of %d cells: %q", block, cells, got, row)
				}
			}
		}
	}
}
