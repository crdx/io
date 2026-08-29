package menu

import (
	"errors"
	"io"
	"os"
	"slices"
	"syscall"
	"testing"

	"crdx.org/io/cmd/oh/key"
)

type fakeList struct {
	rows       []string
	unrunnable []bool
	adjusted   []int
}

func (self *fakeList) Len() int { return len(self.rows) }

func (self *fakeList) IsChoosable(index int) bool {
	return index >= len(self.unrunnable) || !self.unrunnable[index]
}

func (self *fakeList) Text(index int) string { return self.rows[index] }

func (self *fakeList) ColumnHeader(room int) string { return Clip("  Agent  Title", room) }

func (self *fakeList) Row(index int, isChosen bool, room int) string {
	return Clip(Mark(isChosen)+" "+self.rows[index], room)
}

func (self *fakeList) Adjust(index int, direction int) {
	if self.adjusted == nil {
		self.adjusted = make([]int, len(self.rows))
	}

	self.adjusted[index] += direction
}

func listState(rows *fakeList, cursor int) *state {
	self := newState(rows)
	self.cursor = cursor

	return self
}

func rowsNamed(names ...string) *fakeList {
	return &fakeList{rows: names}
}

func defaultState() *state {
	return listState(rowsNamed("first", "second", "third"), 0)
}

func TestClipReturnsNothingWhenThereAreNoColumns(t *testing.T) {
	if got := Clip("session", 0); got != "" {
		t.Errorf("expected no text in no columns, got %q", got)
	}
}

func TestEveryWayOfAbandoningTheChoice(t *testing.T) {
	for name, keypress := range map[string]key.Key{
		"escape":       {Code: key.Escape},
		"csi-u escape": {Code: key.Escape, Mod: key.Ctrl},
		"ctrl+c":       {Code: key.Rune, Value: 'c', Mod: key.Ctrl},
	} {
		if got := defaultState().apply(keypress); got != choiceCancelled {
			t.Errorf("%s: expected the choice to be abandoned, got %v", name, got)
		}
	}
}

func TestAnUnrecognisedSequenceChangesNothing(t *testing.T) {
	if got := defaultState().apply(key.Key{Code: key.Unknown}); got != continuePicking {
		t.Errorf("expected nothing to happen, got %v", got)
	}
}

func TestPlainLettersAreNotACancellation(t *testing.T) {
	for _, value := range []rune{'a', 'c', 'q', 'Q'} {
		if got := defaultState().apply(key.Key{Code: key.Rune, Value: value}); got != continuePicking {
			t.Errorf("%q: expected nothing to happen, got %v", value, got)
		}
	}
}

func TestTheCursorStopsAtEitherEnd(t *testing.T) {
	self := defaultState()

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

func TestTheCursorSkipsRowsThatCannotBeChosen(t *testing.T) {
	rows := &fakeList{
		rows:       []string{"one", "two", "three", "four", "five"},
		unrunnable: []bool{true, false, true, false, true},
	}
	self := listState(rows, 1)

	self.apply(key.Key{Code: key.Down})
	if self.cursor != 3 {
		t.Errorf("expected the next row that can be chosen, got %d", self.cursor)
	}
	self.apply(key.Key{Code: key.Down})
	if self.cursor != 3 {
		t.Errorf("expected the cursor to stop at the last row that can be chosen, got %d", self.cursor)
	}
	self.apply(key.Key{Code: key.Home})
	if self.cursor != 1 {
		t.Errorf("expected home to choose the first row that can be chosen, got %d", self.cursor)
	}
	self.apply(key.Key{Code: key.End})
	if self.cursor != 3 {
		t.Errorf("expected end to choose the last row that can be chosen, got %d", self.cursor)
	}
}

func TestARowThatCannotBeChosenIsNotChosen(t *testing.T) {
	self := listState(&fakeList{rows: []string{"one"}, unrunnable: []bool{true}}, 0)
	if got := self.apply(key.Key{Code: key.Enter}); got != continuePicking {
		t.Errorf("expected the row to be ignored, got %v", got)
	}
	if got := self.firstSelectable(); got != -1 {
		t.Errorf("expected no initial choice, got %d", got)
	}
}

func TestTypingNarrowsTheListToWhatWasTyped(t *testing.T) {
	self := listState(rowsNamed(
		"chewy-sardine why the spinner stutters",
		"thick-poodle reasoning traces",
		"funny-badger the spinner again",
	), 0)

	for _, value := range []rune{'s', 'p', 'i', 'n'} {
		self.apply(key.Key{Code: key.Rune, Value: value})
	}

	if self.query != "spin" {
		t.Errorf("expected what was typed, got %q", self.query)
	}
	if !slices.Equal(self.matches, []int{0, 2}) {
		t.Errorf("expected the rows that say it, got %v", self.matches)
	}

	self.apply(key.Key{Code: key.Down})
	if self.chosen() != 2 {
		t.Errorf("expected the second of the narrowed rows, got %d", self.chosen())
	}

	self.apply(key.Key{Code: key.Backspace})
	if self.query != "spi" {
		t.Errorf("expected the last letter to be taken back, got %q", self.query)
	}
	if self.chosen() != 2 {
		t.Errorf("expected the cursor to stay where it was, got %d", self.chosen())
	}
}

func TestEveryWordTypedMustAppearSomewhereInTheRow(t *testing.T) {
	self := listState(rowsNamed(
		"chewy-sardine why the spinner stutters",
		"thick-poodle the spinner is fine",
	), 0)
	self.narrow("poodle spinner")

	if !slices.Equal(self.matches, []int{1}) {
		t.Errorf("expected the row both words describe, got %v", self.matches)
	}
}

func TestAFilterThatMatchesNothingLeavesNothingToChoose(t *testing.T) {
	self := listState(rowsNamed("chewy-sardine"), 0)
	self.narrow("nothing at all")

	if self.cursor != -1 {
		t.Errorf("expected no cursor, got %d", self.cursor)
	}
	if got := self.apply(key.Key{Code: key.Enter}); got != continuePicking {
		t.Errorf("expected nothing to be chosen, got %v", got)
	}

	self.narrow("")
	if self.cursor != 0 {
		t.Errorf("expected the cursor back on the only row, got %d", self.cursor)
	}
}

func TestTakingBackWhatWasNeverTypedIsNothing(t *testing.T) {
	self := listState(rowsNamed("chewy-sardine"), 0)
	self.apply(key.Key{Code: key.Backspace})

	if self.query != "" {
		t.Errorf("expected an empty filter, got %q", self.query)
	}
}

func TestAControlSequenceIsNotTypedIntoTheFilter(t *testing.T) {
	self := listState(rowsNamed("chewy-sardine"), 0)
	self.apply(key.Key{Code: key.Rune, Value: 'a', Mod: key.Alt})

	if self.query != "" {
		t.Errorf("expected an empty filter, got %q", self.query)
	}
}

func TestAnEmptyListIsNothingToChooseFrom(t *testing.T) {
	if _, err := Choose(&fakeList{}, nil, nil); !errors.Is(err, ErrCancelled) {
		t.Errorf("expected the choice to be abandoned, got %v", err)
	}
}

type drawnScreen chan struct{}

func (self drawnScreen) Write(written []byte) (int, error) {
	self <- struct{}{}
	return len(written), nil
}

func TestTheListIsDrawnAgainWhenTheTerminalChangesSize(t *testing.T) {
	self := listState(rowsNamed("chewy-sardine"), 0)

	drawn := make(drawnScreen)
	self.screen = drawn
	self.measure = func() (int, int) { return 80, 24 }

	keys := make(chan key.Key)
	self.keys = keys

	resizes := make(chan os.Signal, 1)
	resizes <- syscall.SIGWINCH

	finished := make(chan struct{})
	go func() {
		defer close(finished)
		if _, err := self.pick(resizes); !errors.Is(err, ErrCancelled) {
			t.Errorf("expected the choice to be abandoned, got %v", err)
		}
	}()

	<-drawn
	<-drawn

	keys <- key.Key{Code: key.Escape}
	<-finished
}

func TestTheChoiceEndsWhenThereIsNothingLeftToRead(t *testing.T) {
	self := listState(rowsNamed("chewy-sardine"), 0)
	self.screen = io.Discard
	self.measure = func() (int, int) { return 80, 24 }

	keys := make(chan key.Key)
	close(keys)
	self.keys = keys

	if _, err := self.pick(nil); !errors.Is(err, ErrCancelled) {
		t.Errorf("expected the choice to be abandoned, got %v", err)
	}
}

func TestAPageMovesTheCursorByWhatIsOnScreen(t *testing.T) {
	rows := make([]string, 20)
	for i := range rows {
		rows[i] = "able-dolphin"
	}

	self := listState(&fakeList{rows: rows}, 0)
	self.window = 5

	self.apply(key.Key{Code: key.PageDown})
	if self.cursor != 5 {
		t.Errorf("expected a page down the list, got %d", self.cursor)
	}

	self.apply(key.Key{Code: key.PageUp})
	if self.cursor != 0 {
		t.Errorf("expected a page back up the list, got %d", self.cursor)
	}

	self.apply(key.Key{Code: key.PageUp})
	if self.cursor != 0 {
		t.Errorf("expected the cursor to stop at the top, got %d", self.cursor)
	}

	for range 5 {
		self.apply(key.Key{Code: key.PageDown})
	}
	if self.cursor != len(rows)-1 {
		t.Errorf("expected the cursor to stop at the bottom, got %d", self.cursor)
	}
}

func TestAPageIsAtLeastOneRowWhenNothingHasBeenDrawn(t *testing.T) {
	self := listState(rowsNamed("one", "two", "three"), 0)

	self.apply(key.Key{Code: key.PageDown})
	if self.cursor != 1 {
		t.Errorf("expected a single row of movement, got %d", self.cursor)
	}
}
