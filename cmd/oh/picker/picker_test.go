package picker

import (
	"errors"
	"testing"

	"crdx.org/io/cmd/oh/key"
)

func pickerState() *state {
	sessions := make([]*Session, 3)

	for i := range sessions {
		sessions[i] = &Session{}
	}

	return &state{sessions: sessions}
}

func TestEveryWayOfAbandoningTheChoice(t *testing.T) {
	for name, keypress := range map[string]key.Key{
		"escape":       {Code: key.Escape},
		"csi-u escape": {Code: key.Escape, Mod: key.Ctrl},
		"q":            {Code: key.Rune, Value: 'q'},
		"ctrl+c":       {Code: key.Rune, Value: 'c', Mod: key.Ctrl},
	} {
		if got := pickerState().apply(keypress); got != choiceCancelled {
			t.Errorf("%s: expected the choice to be abandoned, got %v", name, got)
		}
	}
}

func TestAnUnrecognisedSequenceChangesNothing(t *testing.T) {
	if got := pickerState().apply(key.Key{Code: key.Unknown}); got != continuePicking {
		t.Errorf("expected nothing to happen, got %v", got)
	}
}

func TestPlainLettersAreNotACancellation(t *testing.T) {
	for _, value := range []rune{'a', 'c', 'Q'} {
		if got := pickerState().apply(key.Key{Code: key.Rune, Value: value}); got != continuePicking {
			t.Errorf("%q: expected nothing to happen, got %v", value, got)
		}
	}
}

func TestTheCursorStopsAtEitherEnd(t *testing.T) {
	self := pickerState()

	for range 5 {
		self.apply(key.Key{Code: key.Up})
	}

	if self.cursor != 0 {
		t.Errorf("expected the first row, got %d", self.cursor)
	}

	for range 5 {
		self.apply(key.Key{Code: key.Down})
	}

	if self.cursor != 2 {
		t.Errorf("expected the last row, got %d", self.cursor)
	}
}

func TestTheCursorSkipsRunningSessions(t *testing.T) {
	self := &state{
		sessions: []*Session{
			{IsRunning: true},
			{},
			{IsRunning: true},
			{},
			{IsRunning: true},
		},
		cursor: 1,
	}

	self.apply(key.Key{Code: key.Down})
	if self.cursor != 3 {
		t.Errorf("expected the next available session, got %d", self.cursor)
	}
	self.apply(key.Key{Code: key.Down})
	if self.cursor != 3 {
		t.Errorf("expected the cursor to stop at the last available session, got %d", self.cursor)
	}
	self.apply(key.Key{Code: key.Home})
	if self.cursor != 1 {
		t.Errorf("expected home to choose the first available session, got %d", self.cursor)
	}
	self.apply(key.Key{Code: key.End})
	if self.cursor != 3 {
		t.Errorf("expected end to choose the last available session, got %d", self.cursor)
	}
}

func TestARunningSessionCannotBeChosen(t *testing.T) {
	self := &state{sessions: []*Session{{IsRunning: true}}}
	if got := self.apply(key.Key{Code: key.Enter}); got != continuePicking {
		t.Errorf("expected the running session to be ignored, got %v", got)
	}
	if got := firstSelectable(self.sessions); got != -1 {
		t.Errorf("expected no initial choice, got %d", got)
	}
}

func TestAnEmptyListIsNothingToChooseFrom(t *testing.T) {
	if _, err := Choose(nil, "", nil, nil); !errors.Is(err, ErrCancelled) {
		t.Errorf("expected the choice to be abandoned, got %v", err)
	}
}
