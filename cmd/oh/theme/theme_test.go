package theme

import (
	"strings"
	"testing"

	"crdx.org/col"
)

func TestTheShellPromptUsesTheTerminalsANSIBlue(t *testing.T) {
	originalColorEnabled := colorEnabled
	colorEnabled = true
	t.Cleanup(func() { colorEnabled = originalColorEnabled })

	if got := Shell("$"); !strings.Contains(got, "\x1b[34m$") {
		t.Errorf("got %q, want the standard ANSI blue", got)
	}
}

func TestAUserMessageHasABackgroundThatSurvivesInnerStyles(t *testing.T) {
	originalColorEnabled := colorEnabled
	colorEnabled = true

	t.Cleanup(func() { colorEnabled = originalColorEnabled })

	got := User("before " + Code("inside") + " after")

	if count := strings.Count(got, "\x1b[48;2;"); count < 2 {
		t.Errorf("expected the background to resume after the inner style, got %q", got)
	}

	if plain := Plain(got); plain != "before inside after" {
		t.Errorf("expected the message text unchanged, got %q", plain)
	}
}

func TestReasoningIsItalic(t *testing.T) {
	originalColorEnabled := colorEnabled
	colorEnabled = true
	col.Enable()

	t.Cleanup(func() {
		colorEnabled = originalColorEnabled
		col.Disable()
	})

	got := Reasoning("looking %s", "here")

	if !strings.Contains(got, "\x1b[3m") {
		t.Errorf("expected italic reasoning, got %q", got)
	}

	if plain := Plain(got); plain != "looking here" {
		t.Errorf("expected the reasoning text unchanged, got %q", plain)
	}
}
