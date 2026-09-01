package picker

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

func TestOnlyASessionThatIsNotRunningCanBeArchived(t *testing.T) {
	var archived []string
	self := &sessionList{
		sessions: []*Session{{Name: "chewy-sardine", IsRunning: true}, {Name: "thick-poodle"}},
		archive: func(storedSession *Session) error {
			archived = append(archived, storedSession.Name)
			return nil
		},
	}

	if self.IsRemovable(0) {
		t.Error("expected a running session to be left alone")
	}
	if !self.IsRemovable(1) {
		t.Error("expected an ended session to be archivable")
	}
	if got := self.RemovalPrompt(1); got != "Archive 🐩 thick-poodle?" {
		t.Errorf("unexpected prompt: %q", got)
	}

	if err := self.Remove(1); err != nil {
		t.Fatal(err)
	}
	if len(archived) != 1 || archived[0] != "thick-poodle" {
		t.Errorf("got the sessions archived as %v", archived)
	}
	if self.Len() != 1 {
		t.Errorf("expected the archived session to leave the list, got %d rows", self.Len())
	}
}

func TestASessionPickerWithNowhereToArchiveToRemovesNothing(t *testing.T) {
	self := &sessionList{sessions: []*Session{{Name: "thick-poodle"}}}

	if self.IsRemovable(0) {
		t.Error("expected no session to be archivable without an archiver")
	}
}
