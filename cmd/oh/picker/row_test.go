package picker

import (
	"strings"
	"testing"

	"crdx.org/io/cmd/oh/style"
)

func TestClipReturnsNothingWhenThereAreNoColumns(t *testing.T) {
	if got := clip("session", 0); got != "" {
		t.Errorf("expected no text in no columns, got %q", got)
	}
}

func TestARunningSessionHasAYellowMarker(t *testing.T) {
	self := &state{sessions: []*Session{{IsRunning: true}, {}}}

	if got := self.row(0, 80); !strings.Contains(got, "🟡") {
		t.Errorf("expected the running marker, got %q", got)
	}
	if got := self.row(1, 80); strings.Contains(got, "🟡") {
		t.Errorf("expected no running marker, got %q", got)
	}
}

func TestAnUntitledSessionDoesNotPutAnEscapeSequenceThroughTheClipper(t *testing.T) {
	got := title(&Session{})

	if strings.ContainsRune(got, '\x1b') {
		t.Errorf("expected an unpainted title, got %q", got)
	}

	if style.Width(got) != len("(untitled)") {
		t.Errorf("expected the placeholder title, got %q", got)
	}
}
