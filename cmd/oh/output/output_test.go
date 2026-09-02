package output_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/io/cmd/oh/output"
	"crdx.org/io/cmd/oh/style"
)

func TestTextSizingSupportIsRemembered(t *testing.T) {
	screen := output.New(&bytes.Buffer{})
	if screen.IsTextSizingSupported() {
		t.Fatal("new screen unexpectedly supports text sizing")
	}

	screen.SetTextSizingSupported(true)
	if !screen.IsTextSizingSupported() {
		t.Error("screen forgot text sizing support")
	}
}

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

func TestNoticesRunTogether(t *testing.T) {
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

func TestOutputRunsTogetherExactlyWhenItsGroupMatches(t *testing.T) {
	type kind struct {
		name string
		draw func(*output.Screen, string)
	}

	kinds := []kind{
		{
			name: "work",
			draw: func(screen *output.Screen, text string) {
				screen.DrawReasoning([]string{text})
				screen.Seal()
			},
		},
		{
			name: "notice",
			draw: func(screen *output.Screen, text string) {
				screen.Line(text)
			},
		},
		{
			name: "answer",
			draw: func(screen *output.Screen, text string) {
				screen.DrawAnswer([]string{text})
				screen.Seal()
			},
		},
	}

	for _, first := range kinds {
		for _, second := range kinds {
			t.Run(first.name+"-then-"+second.name, func(t *testing.T) {
				var screenOutput bytes.Buffer
				screen := output.New(&screenOutput)

				first.draw(screen, "one")
				second.draw(screen, "two")

				separator := "\n\n"
				if first.name == second.name {
					separator = "\n"
				}
				if got, want := screenOutput.String(), "one"+separator+"two"; got != want {
					t.Errorf("got %q, want %q", got, want)
				}
			})
		}
	}
}

type fixedBlock string

func (self fixedBlock) Rows(_ int) []string {
	return []string{string(self)}
}

func TestNoticesInsideLiveWorkFollowTheSameGroupingRule(t *testing.T) {
	var screenOutput bytes.Buffer
	screen := output.New(&screenOutput)

	screen.Open(fixedBlock("work"))
	screen.Line("notice one")
	screen.Line("notice two")
	screen.Seal()

	if got, want := screenOutput.String(), "work\n\nnotice one\nnotice two"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

const (
	appendOnlyColumns = 40
	appendOnlyLines   = 24
)

func appendOnlyScreen(writer *bytes.Buffer) *output.Screen {
	return output.NewTerminalOfSize(writer, appendOnlyColumns, appendOnlyLines).AppendOnly()
}

func TestAnAppendOnlyScreenWritesOnlyTheRowsTheLiveRegionSettledOn(t *testing.T) {
	var screenOutput bytes.Buffer

	screen := appendOnlyScreen(&screenOutput)
	screen.DrawAnswer([]string{"one", "two"})
	screen.DrawAnswer([]string{"one", "two", "three"})
	screen.End()

	if got := screenOutput.String(); got != "one\r\ntwo\r\nthree\r\n" {
		t.Errorf("got %q", got)
	}
}

func TestAnAppendOnlyScreenWritesNoEscapeSequences(t *testing.T) {
	var screenOutput bytes.Buffer

	screen := appendOnlyScreen(&screenOutput)
	screen.BeginEditing()
	screen.ReportProgress(true)
	screen.DrawReasoning([]string{"thinking"})
	screen.DrawAnswer([]string{"answered"})
	screen.Footer([]string{"input"}, 0, 0)
	screen.End()
	screen.Release(true)

	if got := screenOutput.String(); strings.Contains(got, "\x1b[") {
		t.Errorf("expected no control sequences, got %q", got)
	}
}

func TestAnAppendOnlyTerminalStillLinksThePathsItNames(t *testing.T) {
	workspaceDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspaceDir, "one.go"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	var screenOutput bytes.Buffer
	screen := appendOnlyScreen(&screenOutput).LinkPathsUnder(workspaceDir)
	screen.DrawAnswer([]string{"see one.go"})
	screen.End()

	if got := screenOutput.String(); !strings.Contains(got, "\x1b]8;;file://") {
		t.Errorf("expected the answer to be linked, got %q", got)
	}
}
