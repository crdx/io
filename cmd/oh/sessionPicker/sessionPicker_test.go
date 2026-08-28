package sessionPicker

import (
	"strings"
	"testing"

	"crdx.org/io/cmd/oh/style"
)

func TestARunningSessionIsFadedAndSlantedWithoutAMarker(t *testing.T) {
	self := &sessionList{sessions: []*Session{{IsRunning: true}, {}}}
	got := self.Row(0, false, 80)
	want := style.Running(row(self.sessions[0], false, 80))

	if got != want {
		t.Errorf("expected a faded row, got %q", got)
	}
	if strings.Contains(got, "🟡") {
		t.Errorf("expected no running marker, got %q", got)
	}
}

func TestASessionAnimalIncludesItsNameAndEmoji(t *testing.T) {
	got := sessionAnimal(&Session{Name: "chewy-sardine"})
	if got != "🐟 chewy-sardine" {
		t.Errorf("unexpected session animal: %q", got)
	}
}

func TestAnUntitledSessionDoesNotPutAnEscapeSequenceThroughTheClipper(t *testing.T) {
	got := sessionTitle(&Session{})

	if strings.ContainsRune(got, '\x1b') {
		t.Errorf("expected an unpainted title, got %q", got)
	}

	if style.Width(got) != len("(untitled)") {
		t.Errorf("expected the placeholder title, got %q", got)
	}
}
