package output_test

import (
	"bytes"
	"strings"
	"testing"

	"crdx.org/io/cmd/oh/output"
	"crdx.org/io/cmd/oh/style"
)

func TestAFinishedTurnEndsWithANewline(t *testing.T) {
	var screenOutput bytes.Buffer

	screen := output.New(&screenOutput)

	screen.Line("banner")
	screen.DrawAnswer([]string{style.Answer("hello")})
	screen.End()

	if got := screenOutput.String(); !strings.HasSuffix(got, "\n") {
		t.Errorf("expected the final message to end with a newline, got %q", got)
	}
}

func TestTheNextThingSaidStartsTheLineTheTurnCameDownTo(t *testing.T) {
	var screenOutput bytes.Buffer

	screen := output.New(&screenOutput)

	screen.Line("banner")
	screen.DrawAnswer([]string{style.Answer("hello")})
	screen.End()
	screen.Line("> again")

	if got := screenOutput.String(); got != "banner\n\n"+style.Answer("hello")+"\n\n> again" {
		t.Errorf("expected an answer set apart from each, got %q", got)
	}
}

func TestEndingATurnTwiceComesDownOnlyOnce(t *testing.T) {
	var screenOutput bytes.Buffer

	screen := output.New(&screenOutput)

	screen.DrawAnswer([]string{style.Answer("hello")})
	screen.End()
	screen.End()
	screen.Line("> again")

	if got := screenOutput.String(); got != style.Answer("hello")+"\n\n> again" {
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

func TestAnAnswerKeepsTheBlankRowsInsideIt(t *testing.T) {
	var screenOutput bytes.Buffer

	screen := output.New(&screenOutput)

	screen.DrawAnswer([]string{style.Answer("one"), "", style.Answer("two")})
	screen.End()

	want := style.Answer("one") + "\n\n" + style.Answer("two") + "\n"

	if got := screenOutput.String(); got != want {
		t.Errorf("expected the paragraph break to stay, got %q", got)
	}
}
