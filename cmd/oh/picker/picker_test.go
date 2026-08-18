package picker

import (
	"errors"
	"testing"

	"crdx.org/io/cmd/oh/key"
	"crdx.org/io/cmd/oh/store"
)

func pickerState() *state {
	sessions := make([]*store.Session, 3)

	for index := range sessions {
		sessions[index] = &store.Session{}
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

func TestAnEmptyListIsNothingToChooseFrom(t *testing.T) {
	if _, err := Session(nil, nil, nil); !errors.Is(err, ErrCancelled) {
		t.Errorf("expected the choice to be abandoned, got %v", err)
	}
}
