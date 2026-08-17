package output_test

import (
	"bytes"
	"strings"
	"testing"

	"crdx.org/io/cmd/oh/output"
	"crdx.org/io/cmd/oh/theme"
)

// A finished turn leaves its final line terminated, rather than leaving the terminal cursor parked
// directly after its last character.
func TestAFinishedTurnEndsWithANewline(t *testing.T) {
	var screenOutput bytes.Buffer

	screen := output.New(&screenOutput)

	screen.Line("banner")
	screen.Answer("hello")
	screen.End()

	if got := screenOutput.String(); !strings.HasSuffix(got, "\n") {
		t.Errorf("expected the final message to end with a newline, got %q", got)
	}
}

// The line a finished turn came down to is started by whatever goes on it next, and started once.
func TestTheNextThingSaidStartsTheLineTheTurnCameDownTo(t *testing.T) {
	var screenOutput bytes.Buffer

	screen := output.New(&screenOutput)

	screen.Line("banner")
	screen.Answer("hello")
	screen.End()
	screen.Line("> again")

	if got := screenOutput.String(); got != "banner\n\n"+theme.Answer("hello")+"\n\n> again" {
		t.Errorf("expected an answer set apart from each, got %q", got)
	}
}

func TestEndingATurnTwiceComesDownOnlyOnce(t *testing.T) {
	var screenOutput bytes.Buffer

	screen := output.New(&screenOutput)

	screen.Answer("hello")
	screen.End()
	screen.End()
	screen.Line("> again")

	if got := screenOutput.String(); got != theme.Answer("hello")+"\n\n> again" {
		t.Errorf("expected one line between each, got %q", got)
	}
}

func TestEndingATurnThatSaidNothingComesDownNowhere(t *testing.T) {
	var screenOutput bytes.Buffer

	screen := output.New(&screenOutput)

	screen.End()
	screen.Line("> again")

	if got := screenOutput.String(); got != "> again" {
		t.Errorf("expected nothing before it, got %q", got)
	}
}

// A line asked to stand apart is set off from what came before it and from what comes after, which
// is one empty line either side and not two between it and the next.
func TestALineAskedToStandApartIsSetOffOnBothSides(t *testing.T) {
	var screenOutput bytes.Buffer

	screen := output.New(&screenOutput)

	screen.Line("before")
	screen.Blank()
	screen.Line("> hello")
	screen.Blank()
	screen.Line("after")

	if got := screenOutput.String(); got != "before\n\n> hello\n\nafter" {
		t.Errorf("expected an empty line either side, got %q", got)
	}
}

// An empty line stands between two things said, so the first thing said has nothing to stand apart
// from and starts where it would have anyway.
func TestNothingIsSetOffFromNothing(t *testing.T) {
	var screenOutput bytes.Buffer

	screen := output.New(&screenOutput)

	screen.Blank()
	screen.Line("> hello")

	if got := screenOutput.String(); got != "> hello" {
		t.Errorf("expected the conversation to open where it opens, got %q", got)
	}
}

func TestAskingForTheSameEmptyLineTwiceLeavesOne(t *testing.T) {
	var screenOutput bytes.Buffer

	screen := output.New(&screenOutput)

	screen.Line("before")
	screen.Blank()
	screen.Blank()
	screen.Line("after")

	if got := screenOutput.String(); got != "before\n\nafter" {
		t.Errorf("expected one empty line, got %q", got)
	}
}

// What the model says stands apart from what it did to find it out, and arrives in as many pieces
// as it likes: the answer is one thing, not one per delta.
func TestAnAnswerIsSetApartHoweverManyPiecesItArrivesIn(t *testing.T) {
	var screenOutput bytes.Buffer

	screen := output.New(&screenOutput)

	screen.Line("read main.go")

	for _, delta := range []string{"one ", "two ", "three"} {
		screen.Answer(delta)
	}

	screen.Line("read go.mod")

	want := "read main.go\n\n" +
		theme.Answer("one ") + theme.Answer("two ") + theme.Answer("three") +
		"\n\nread go.mod"

	if got := screenOutput.String(); got != want {
		t.Errorf("expected the answer set apart, got %q", got)
	}
}

// Rows follow one another without a gap: they are one thing said between them, not a row each.
func TestLinesThatAreNotAnswersRunTogether(t *testing.T) {
	var screenOutput bytes.Buffer

	screen := output.New(&screenOutput)

	screen.Line("read main.go")
	screen.Line("read go.mod")
	screen.Line("read TODO.md")

	if got := screenOutput.String(); got != "read main.go\nread go.mod\nread TODO.md" {
		t.Errorf("expected the rows to run together, got %q", got)
	}
}

// A model that ends its reply on a newline has written the line that ends it, and one more empty
// line is the gap: two would be the gap and a blank row nobody asked for.
func TestAnAnswerEndingInNewlinesIsNotPushedFurtherApart(t *testing.T) {
	for _, trailingNewlines := range []string{"", "\n", "\n\n", "\n\n\n"} {
		var screenOutput bytes.Buffer

		screen := output.New(&screenOutput)

		screen.Answer("hello" + trailingNewlines)
		screen.Line("read main.go")

		if got := screenOutput.String(); !strings.HasSuffix(got, "\n\nread main.go") {
			t.Errorf("%q: expected one empty line before the row, got %q", trailingNewlines, got)
		}

		if strings.Contains(screenOutput.String(), "\n\n\n") {
			t.Errorf("%q: expected no empty line to double up, got %q", trailingNewlines, screenOutput.String())
		}
	}
}

// A reply that opens on newlines opens on nothing, and is put where it was going anyway.
func TestAnAnswerOpeningOnNewlinesIsNotPushedFurtherApart(t *testing.T) {
	var screenOutput bytes.Buffer

	screen := output.New(&screenOutput)

	screen.Line("read main.go")
	screen.Answer("\n")
	screen.Answer("\n\nhello")

	if got := screenOutput.String(); got != "read main.go\n\n"+theme.Answer("hello") {
		t.Errorf("expected the answer where it was going anyway, got %q", got)
	}
}

// A blank row in the middle of a reply is the model's own paragraph, and stays.
func TestAnAnswerKeepsTheBlankRowsInsideIt(t *testing.T) {
	var screenOutput bytes.Buffer

	screen := output.New(&screenOutput)

	screen.Answer("one")
	screen.Answer("\n\n")
	screen.Answer("two")

	want := theme.Answer("one") + "\n\n" + theme.Answer("two")

	if got := screenOutput.String(); got != want {
		t.Errorf("expected the paragraph break to stay, got %q", got)
	}
}
