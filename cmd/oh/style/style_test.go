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

func TestDisabledCapabilitiesAreDimmedOverTheirMutedColour(t *testing.T) {
	enableColor(t)

	got := Withheld("w")

	if !strings.Contains(got, "\x1b[2m") {
		t.Errorf("expected a dim disabled capability, got %q", got)
	}

	if !strings.Contains(got, "\x1b[38;2;") {
		t.Errorf("expected the dimming on top of the muted colour, got %q", got)
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

func TestNothingIsPaintedWhereTheScreenIsNotATerminal(t *testing.T) {
	t.Cleanup(Init(&strings.Builder{})) // anything that is not a file is not a terminal

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
