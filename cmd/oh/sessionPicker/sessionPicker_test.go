package sessionPicker

import (
	"strings"
	"testing"
	"time"

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

func TestSessionLengthUsesCompactUnits(t *testing.T) {
	cases := map[time.Duration]string{
		0:                           "<1m",
		59 * time.Second:            "<1m",
		37 * time.Minute:            "37m",
		5*time.Hour + 1*time.Minute: "5h",
		73 * time.Hour:              "3d",
	}

	for elapsed, want := range cases {
		if got := duration(elapsed); got != want {
			t.Errorf("duration(%s) = %q, want %q", elapsed, got, want)
		}
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
