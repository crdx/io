package markdown

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"crdx.org/io/cmd/oh/style"
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

func TestAnAnswerDrawnADeltaAtATimeIsTheAnswerDrawnAtOnce(t *testing.T) {
	var last []string

	for length := range len(answer) + 1 {
		last = Render(answer[:length], columns)

		for _, row := range last {
			if got := style.Width(row); got > columns {
				t.Fatalf("a prefix of %d gave a row of %d cells: %q", length, got, row)
			}
		}
	}

	if want := Render(answer, columns); !slices.Equal(last, want) {
		t.Errorf("the last drawing was %q, want %q", last, want)
	}
}

func TestATableWidensAsItsRowsArrive(t *testing.T) {
	short := Render("| a | b |\n|---|---|\n| x | y |\n", columns)
	long := Render("| a | b |\n|---|---|\n| x | y |\n| a much wider cell | y |\n", columns)

	if style.Width(long[0]) <= style.Width(short[0]) {
		t.Errorf("expected the table to widen, got %q and %q", short[0], long[0])
	}

	for _, row := range long {
		if style.Width(row) != style.Width(long[0]) {
			t.Errorf("expected every row the same width, got %q against %q", row, long[0])
		}
	}
}

func TestATableTooNarrowForItsColumnsIsAbandoned(t *testing.T) {
	source := "| a | b |\n|---|---|\n| x | y |"

	got := style.Plain(strings.Join(Render(source, 8), " "))

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

	if len(got) != 1 || !strings.Contains(style.Plain(got[0]), "func main() {") {
		t.Errorf("expected the open block to be drawn as code, got %q", got)
	}

	if strings.HasPrefix(style.Plain(got[0]), " ") {
		t.Errorf("expected the code to start at the left edge, got %q", got[0])
	}
}

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
		got := style.Plain(strings.Join(Render(text, columns), "\n"))

		if got != want {
			t.Errorf("Render(%q) drew %q, want %q", text, got, want)
		}
	}
}

func TestANestedListKeepsItsChildrenWithTheirParent(t *testing.T) {
	source := "- `one`\n\n  - child\n  - another\n- `two`\n\n  - child"
	got := style.Plain(strings.Join(Render(source, columns), "\n"))
	want := "• one\n  • child\n  • another\n• two\n  • child"

	if got != want {
		t.Errorf("nested list drew %q, want %q", got, want)
	}
}

func TestTheBlocksAreDrawnWithoutTheirPunctuation(t *testing.T) {
	got := style.Plain(strings.Join(Render(answer, columns), "\n"))

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

func TestAFenceIsClosedOnlyByALineThatIsAFence(t *testing.T) {
	got := style.Plain(strings.Join(Render("```\none\n```go still open\ntwo\n```\nafter", columns), "\n"))

	for _, want := range []string{"one", "```go still open", "two", "after"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q to survive, got %q", want, got)
		}
	}
}

func TestAStyleInsideAnotherKeepsTheOneOutsideIt(t *testing.T) {
	got := Render("**one `two` three**", columns)[0]

	if strings.Count(got, "\x1b[1m") < 2 {
		t.Errorf("expected the bold to be opened again after the inner style, got %q", got)
	}
}

func TestATableHeadingIsDrawnAsMarkdownRatherThanShown(t *testing.T) {
	got := style.Plain(strings.Join(Render("| **Name** | `Kind` |\n|---|---|\n| a | b |", columns), "\n"))

	if strings.Contains(got, "**") || strings.Contains(got, "`") {
		t.Errorf("expected the heading punctuation to be drawn, got %q", got)
	}
}

func TestCodeIsHighlightedWhereTheLanguageIsKnown(t *testing.T) {
	known := Render("```go\nfunc main() {}\n```", columns)
	unknown := Render("```nosuchlanguage\nfunc main() {}\n```", columns)

	if style.Plain(known[0]) != style.Plain(unknown[0]) {
		t.Errorf("expected the same code either way, got %q and %q", known[0], unknown[0])
	}

	if known[0] == unknown[0] {
		t.Errorf("expected the known language to be highlighted, got %q", known[0])
	}

	if strings.Count(unknown[0], "\x1b[") != 2 {
		t.Errorf("expected the unknown language to be drawn as one run, got %q", unknown[0])
	}
}

func TestBashCommandNamesAndFirstParametersAreHighlighted(t *testing.T) {
	for name, test := range map[string]struct {
		source string
		want   string
	}{
		"simple": {
			"go test ./cmd/oh",
			style.Function("go") + style.Block(" ") + style.Function("test") + style.Block(" ./cmd/oh"),
		},
		"assignment": {
			"GOCACHE=/tmp/io-go-cache go list",
			style.Block("GOCACHE") + style.Operator("=") + style.Block("/tmp/io-go-cache") +
				style.Block(" ") + style.Function("go") + style.Block(" ") + style.Function("list"),
		},
		"pipeline": {
			"printf one | grep one",
			style.Function("printf") + style.Block(" ") + style.Function("one") + style.Block(" ") +
				style.Operator("|") + style.Block(" ") + style.Function("grep") + style.Block(" ") +
				style.Function("one"),
		},
		"combined output pipeline": {
			"printf one |& grep one",
			style.Function("printf") + style.Block(" ") + style.Function("one") + style.Block(" ") +
				style.Operator("|&") + style.Block(" ") + style.Function("grep") + style.Block(" ") +
				style.Function("one"),
		},
		"background": {
			"sleep 1 & wait",
			style.Function("sleep") + style.Block(" ") + style.Function("1") + style.Block(" ") +
				style.Operator("&") + style.Block(" ") + style.Function("wait"),
		},
		"and conditional": {
			"go test && git status --short",
			style.Function("go") + style.Block(" ") + style.Function("test") + style.Block(" ") +
				style.Operator("&&") + style.Block(" ") + style.Function("git") + style.Block(" ") +
				style.Function("status") + style.Block(" --short"),
		},
		"or conditional": {
			"go test || git status --short",
			style.Function("go") + style.Block(" ") + style.Function("test") + style.Block(" ") +
				style.Operator("||") + style.Block(" ") + style.Function("git") + style.Block(" ") +
				style.Function("status") + style.Block(" --short"),
		},
		"command substitution": {
			"echo one $(go list)",
			style.Function("echo") + style.Block(" ") + style.Function("one") + style.Block(" $(") +
				style.Function("go") + style.Block(" ") + style.Function("list") + style.Block(")"),
		},
		"executable only": {
			"true",
			style.Function("true"),
		},
	} {
		if got := Highlight(test.source, "bash"); got != test.want {
			t.Errorf("%s: got %q, want %q", name, got, test.want)
		}
	}
}

func TestMalformedBashFallsBackToOnePlainRun(t *testing.T) {
	source := "if true; then"
	if got, want := Highlight(source, "bash"), style.Block(source); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBashAssignmentsAndRedirectionsAreHighlighted(t *testing.T) {
	source := `PATCH=/tmp/change.patch; : >"$PATCH"`
	want := style.Block("PATCH") + style.Operator("=") + style.Block("/tmp/change.patch") +
		style.Operator(";") + style.Block(" ") + style.Function(":") + style.Block(" ") + style.Operator(">") +
		style.Block(`"$PATCH"`)

	if got := Highlight(source, "bash"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	prefix := `PATCH=/tmp/cha`
	wantPrefix := style.Block("PATCH") + style.Operator("=") + style.Block("/tmp/cha") + style.Block("…")
	if got := HighlightPrefix(source, prefix, "bash", true); got != wantPrefix {
		t.Errorf("elided: got %q, want %q", got, wantPrefix)
	}
}

func TestNestedBashSpansDoNotSliceBackwards(t *testing.T) {
	source := `RESULT=$(printf one)`
	want := style.Block("RESULT") + style.Operator("=") + style.Block("$(printf one)")

	if got := Highlight(source, "bash"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBashForLoopKeywordsAreHighlighted(t *testing.T) {
	source := "for path in one; do true; done"
	want := style.Keyword("for") + style.Block(" path ") + style.Keyword("in") +
		style.Block(" one; ") + style.Keyword("do") +
		style.Block(" ") + style.Function("true") + style.Operator(";") + style.Block(" ") +
		style.Keyword("done")

	if got := Highlight(source, "bash"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBashCompoundKeywordsAreHighlighted(t *testing.T) {
	for name, test := range map[string]struct {
		source   string
		keywords []string
	}{
		"while": {"while true; do echo one; done", []string{"while", "do", "done"}},
		"until": {"until false; do echo one; done", []string{"until", "do", "done"}},
		"if":    {"if true; then echo one; fi", []string{"if", "then", "fi"}},
		"else":  {"if true; then echo one; else echo two; fi", []string{"if", "then", "else", "fi"}},
		"elif":  {"if true; then echo one; elif false; then echo two; fi", []string{"if", "then", "elif", "fi"}},
		"case":  {"case one in one) echo one;; esac", []string{"case", "in", "esac"}},
	} {
		got := Highlight(test.source, "bash")

		for _, keyword := range test.keywords {
			if !strings.Contains(got, style.Keyword(keyword)) {
				t.Errorf("%s: expected %q painted as a keyword, got %q", name, keyword, got)
			}
		}
	}
}

func TestACaseItemTerminatorIsAnOperator(t *testing.T) {
	got := Highlight("case one in one) echo one;; esac", "bash")

	if !strings.Contains(got, style.Operator(";;")) {
		t.Errorf("expected the terminator painted as an operator, got %q", got)
	}
}

func TestRegexpSyntaxIsHighlighted(t *testing.T) {
	source := `^(foo|bar)+\s[0-9]{2,4}$`
	want := style.Keyword("^") + style.Block("(") + style.Block("foo") +
		style.Operator("|") + style.Block("bar") + style.Block(")") +
		style.Operator("+") + style.Keyword(`\s`) + style.Block("[") +
		style.Block("0") + style.Operator("-") + style.Block("9") +
		style.Block("]") + style.Operator("{2,4}") + style.Keyword("$")

	if got := Highlight(source, "regexp"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestARegexpCharacterClassHoldsNoAnchors(t *testing.T) {
	source := `[^0-9]`
	want := style.Block("[") + style.Block("^0") +
		style.Operator("-") + style.Block("9") + style.Block("]")

	if got := Highlight(source, "regexp"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestElidedRegexpKeepsTheStyleOfAPartialEscape(t *testing.T) {
	source := `foo\p{Greek}bar`
	prefix := `foo\p{G`
	want := style.Block("foo") + style.Keyword(`\p{G`) + style.Keyword("…")

	if got := HighlightPrefix(source, prefix, "regexp", true); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNoBlockOverrunsANarrowTerminal(t *testing.T) {
	blocks := []string{
		"```\nabc\n```", "> quoted", "# heading", "- item", "| a | b |\n|---|---|\n| x | y |",
		"a paragraph of several words", "---",
	}

	for _, block := range blocks {
		for cells := 1; cells <= 12; cells++ {
			for _, row := range Render(block, cells) {
				if got := style.Width(row); got > cells {
					t.Errorf("Render(%q, %d) gave a row of %d cells: %q", block, cells, got, row)
				}
			}
		}
	}
}

func TestAStyleOverAnotherLeavesTextAloneWhereNothingIsPainted(t *testing.T) {
	paintsNothing := func(format any, args ...any) string { return fmt.Sprint(format) }

	if got := over(paintsNothing, "hello"); got != "hello" {
		t.Errorf("got %q, want the text left alone where there is no reset to find", got)
	}
}
