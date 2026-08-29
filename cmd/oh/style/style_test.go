package style

import (
	"strings"
	"testing"
)

func enableColor(t *testing.T) {
	t.Helper()

	previous := colorEnabled
	apply(true)

	t.Cleanup(func() { apply(previous) })
}

func TestDisabledCapabilitiesTakeTheMutedColour(t *testing.T) {
	enableColor(t)

	got := Dim("w")

	if strings.Contains(got, "\x1b[2m") {
		t.Errorf("expected no reduced intensity on a disabled capability, got %q", got)
	}

	if !strings.Contains(got, "\x1b[38;2;") {
		t.Errorf("expected the muted colour, got %q", got)
	}
}

func TestTheShellPromptMatchesACommandName(t *testing.T) {
	enableColor(t)

	if got, want := Shell("$"), Function("$"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAUserMessageHasABackgroundThatSurvivesInnerStyles(t *testing.T) {
	enableColor(t)

	got := User("before " + Code("inside") + " after")

	if count := strings.Count(got, "\x1b[48;2;"); count < 2 {
		t.Errorf("expected the background to resume after the inner style, got %q", got)
	}

	if plain := Plain(got); plain != "before inside after" {
		t.Errorf("expected the message text unchanged, got %q", plain)
	}
}

func TestAStyleOverAnotherResumesWhereTheInnerOneReset(t *testing.T) {
	enableColor(t)

	opening := "\x1b[" + sgr(copper) + "m"
	got := Chosen.Over("row " + Qualifier("note") + " tail")

	if count := strings.Count(got, opening); count != 2 {
		t.Errorf("expected the outer style to resume after the inner one, got %q", got)
	}

	if strings.HasSuffix(got, opening+reset) {
		t.Errorf("expected nothing opened at the very end, got %q", got)
	}

	if plain := Plain(got); plain != "row note tail" {
		t.Errorf("expected the row's text unchanged, got %q", plain)
	}
}

func TestAStyleOverAnotherPaintsNothingWhereNothingIsPainted(t *testing.T) {
	t.Cleanup(Init(&strings.Builder{}))

	if got := Chosen.Over("row note"); got != "row note" {
		t.Errorf("got %q, want the text left alone", got)
	}
}

func TestReasoningIsItalic(t *testing.T) {
	enableColor(t)

	got := Reasoning("looking %s", "here")

	if !strings.Contains(got, "\x1b[3m") {
		t.Errorf("expected italic reasoning, got %q", got)
	}

	if plain := Plain(got); plain != "looking here" {
		t.Errorf("expected the reasoning text unchanged, got %q", plain)
	}
}

func TestPlainKeepsTextCarriedByTheTextSizingProtocol(t *testing.T) {
	sized := "\x1b]66;s=2:w=2;🐟\x1b\\"
	if got := Plain("before " + sized + " after"); got != "before 🐟 after" {
		t.Errorf("got %q, want the visible text", got)
	}
	if got := Width(sized); got != 4 {
		t.Errorf("width = %d, want 4", got)
	}
}

func TestNothingIsPaintedWhereTheScreenIsNotATerminal(t *testing.T) {
	t.Cleanup(Init(&strings.Builder{}))

	if colorEnabled {
		t.Fatal("expected colour to be off where the screen is not a terminal")
	}

	for name, paint := range map[string]Style{
		"failure":   Failure,
		"subject":   Subject,
		"reasoning": Reasoning,
		"user":      User,
	} {
		if got := paint("hello"); got != "hello" {
			t.Errorf("%s painted %q, want it left alone", name, got)
		}
	}

	if got := Pending(Read("r")); got != "r" {
		t.Errorf("a style over another painted %q, want it left alone", got)
	}
}

func TestInitPutsTheDecisionBackWhenItsRestoreIsCalled(t *testing.T) {
	enableColor(t)

	restore := Init(&strings.Builder{})

	if colorEnabled {
		t.Fatal("expected colour off while the screen is not a terminal")
	}

	restore()

	if !colorEnabled {
		t.Fatal("expected colour back on once the decision was put back")
	}

	if got := Failure("hello"); got == "hello" {
		t.Errorf("expected painting to resume, got %q", got)
	}
}

func TestJoinDropsEmptyPartsAndSpacesTheRest(t *testing.T) {
	enableColor(t)

	if got, want := Subtle.Join("4L", "", "~2k"), Subtle("4L ~2k"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestJoinPaintsNothingWhenEveryPartIsEmpty(t *testing.T) {
	enableColor(t)

	if got := Subtle.Join("", ""); got != "" {
		t.Errorf("got %q, want an empty string", got)
	}
}
