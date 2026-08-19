package line

import (
	"bufio"
	"slices"
	"strings"
	"testing"
	"time"

	"crdx.org/io/cmd/oh/key"
)

func inputFromKeys(t *testing.T, text string) *Input {
	t.Helper()

	self := NewInput(nil)
	decoder := key.NewDecoder(bufio.NewReader(strings.NewReader(text)))

	for {
		keypress, err := decoder.Next()
		if err != nil {
			return self
		}

		self.Apply(keypress, false)
	}
}

const (
	pasteStart = "\x1b[200~"
	pasteEnd   = "\x1b[201~"
)

func TestAPasteKeepsItsLineBreaks(t *testing.T) {
	for name, payload := range map[string]string{
		"lf":   "one\ntwo\nthree",
		"cr":   "one\rtwo\rthree",
		"crlf": "one\r\ntwo\r\nthree",
	} {
		self := inputFromKeys(t, pasteStart+payload+pasteEnd)

		if got := self.Text(); got != "one\ntwo\nthree" {
			t.Errorf("%s: expected three lines, got %q", name, got)
		}
	}
}

func TestAPastedTabBecomesSpaces(t *testing.T) {
	self := inputFromKeys(t, pasteStart+"a\tb"+pasteEnd)

	if got := self.Text(); got != "a"+strings.Repeat(" ", tabStop)+"b" {
		t.Errorf("expected the tab to be spaces, got %q", got)
	}
}

func TestAPastePreservesRelativeIndentation(t *testing.T) {
	self := inputFromKeys(t, pasteStart+"    one\n        two\n      three"+pasteEnd)

	if got := self.Text(); got != "one\n    two\n  three" {
		t.Errorf("expected the paste left-aligned with relative indentation preserved, got %q", got)
	}
}

func TestAnAlreadyLeftAlignedPasteKeepsItsIndentation(t *testing.T) {
	self := inputFromKeys(t, pasteStart+"one\n    two"+pasteEnd)

	if got := self.Text(); got != "one\n    two" {
		t.Errorf("expected the paste unchanged, got %q", got)
	}
}

func TestAPasteEndsWhereTheTerminalSaysItDoes(t *testing.T) {
	self := inputFromKeys(t, pasteStart+"one"+pasteEnd)

	if got := self.Apply(key.Key{Code: key.Enter}, false); got != Accept {
		t.Errorf("expected the line to be finished after the paste, got %v", got)
	}

	if got := self.Text(); got != "one" {
		t.Errorf("expected the pasted text, got %q", got)
	}
}

func TestReturnOutsideAPasteFinishesTheLine(t *testing.T) {
	self := NewInput(nil)

	self.Apply(key.Key{Code: key.Rune, Value: 'a'}, false)

	if got := self.Apply(key.Key{Code: key.Enter}, false); got != Accept {
		t.Errorf("expected the line to be finished, got %v", got)
	}
}

func TestTwoReturnsOnAnEmptyIdleLineAskToContinue(t *testing.T) {
	self := NewInput(nil)

	if got := self.Apply(key.Key{Code: key.Enter}, false); got != Drawn {
		t.Errorf("expected the first return to do nothing, got %v", got)
	}

	if got := self.Apply(key.Key{Code: key.Enter}, false); got != Continue {
		t.Errorf("expected the second return to continue, got %v", got)
	}
}

func TestReturnAcceptsInputDuringARunningTurn(t *testing.T) {
	self := inputFromKeys(t, "hello")

	if got := self.Apply(key.Key{Code: key.Enter}, true); got != Accept {
		t.Errorf("expected return to accept the input, got %v", got)
	}

	if self.Text() != "hello" {
		t.Errorf("expected what was typed to be left alone, got %q", self.Text())
	}
}

func TestTwoReturnsOnAnEmptyRunningLineAskToContinue(t *testing.T) {
	for name, inputText := range map[string]string{"empty": "", "whitespace": " "} {
		self := inputFromKeys(t, inputText)

		if got := self.Apply(key.Key{Code: key.Enter}, true); got != Drawn {
			t.Errorf("%s: expected the first return to leave the turn running, got %v", name, got)
		}

		if got := self.Apply(key.Key{Code: key.Enter}, true); got != Continue {
			t.Errorf("%s: expected the second return to continue, got %v", name, got)
		}
	}
}

func TestDoubleReturnHasACoolOffBeforeItCanContinueAgain(t *testing.T) {
	now := time.Time{}
	self := NewInput(nil)
	self.currentTime = func() time.Time { return now }

	self.Apply(key.Key{Code: key.Enter}, true)
	if got := self.Apply(key.Key{Code: key.Enter}, true); got != Continue {
		t.Fatalf("expected the first double return to continue, got %v", got)
	}

	self.Reset()
	for range 2 {
		now = now.Add(continueCoolOff * 9 / 10)
		if got := self.Apply(key.Key{Code: key.Enter}, true); got != Drawn {
			t.Errorf("expected a held return to extend the cool-off, got %v", got)
		}
	}

	now = now.Add(continueCoolOff)
	if got := self.Apply(key.Key{Code: key.Enter}, true); got != Drawn {
		t.Errorf("expected the first return after the cool-off to do nothing, got %v", got)
	}
	if got := self.Apply(key.Key{Code: key.Enter}, true); got != Continue {
		t.Errorf("expected a new double return after the cool-off to continue, got %v", got)
	}
}

func TestReturnsMustBeConsecutiveToContinueAnEmptyRunningTurn(t *testing.T) {
	self := NewInput(nil)
	self.Apply(key.Key{Code: key.Enter}, true)
	self.Apply(key.Key{Code: key.Left}, true)

	if got := self.Apply(key.Key{Code: key.Enter}, true); got != Drawn {
		t.Errorf("expected an intervening key to clear the first return, got %v", got)
	}
}

func TestPendingReturnDoesNotSurviveATurnStateChange(t *testing.T) {
	self := NewInput(nil)
	self.Apply(key.Key{Code: key.Enter}, true)

	if got := self.Apply(key.Key{Code: key.Enter}, false); got != Drawn {
		t.Errorf("expected the first idle return to do nothing, got %v", got)
	}
}

func TestShiftReturnOpensALine(t *testing.T) {
	self := NewInput(nil)

	self.Apply(key.Key{Code: key.Rune, Value: 'a'}, true)
	self.Apply(key.Key{Code: key.Enter, Mod: key.Shift}, true)
	self.Apply(key.Key{Code: key.Rune, Value: 'b'}, true)

	if got := self.Text(); got != "a\nb" {
		t.Errorf("expected two lines, got %q", got)
	}
}

func TestWideCharactersWrapByTheCellsTheyTake(t *testing.T) {
	self := NewInput(nil)

	for _, value := range "日本語" {
		self.Apply(key.Key{Code: key.Rune, Value: value}, false)
	}

	frame := self.Frame(5)
	rows, cursorRow, cursorColumn := frame.Rows, frame.Row, frame.Column

	if len(rows) != 2 || rows[0] != "日本" || rows[1] != "語" {
		t.Errorf("expected two characters then one, got %q", rows)
	}

	if cursorRow != 1 || cursorColumn != 2 {
		t.Errorf("expected the cursor after the third character, got row %d column %d", cursorRow, cursorColumn)
	}
}

func TestInputWrapsBetweenWords(t *testing.T) {
	frame := inputFromKeys(t, "one two three").Frame(7)
	want := []string{"one two", "three"}

	if !slices.Equal(frame.Rows, want) {
		t.Errorf("expected words kept together, got %q", frame.Rows)
	}
}

func TestAWrappingSpaceDoesNotStartTheNextRow(t *testing.T) {
	frame := inputFromKeys(t, "one two").Frame(4)
	want := []string{"one", "two"}

	if !slices.Equal(frame.Rows, want) {
		t.Errorf("expected the wrapping space hidden, got %q", frame.Rows)
	}
}

func TestAWordWiderThanTheInputStillWraps(t *testing.T) {
	frame := inputFromKeys(t, "abcdef").Frame(4)
	want := []string{"abcd", "ef"}

	if !slices.Equal(frame.Rows, want) {
		t.Errorf("expected the long word to wrap, got %q", frame.Rows)
	}
}

func TestLeadingWhitespaceKeepsItsRoom(t *testing.T) {
	frame := (&Input{buffer: &buffer{runes: []rune("    one")}}).Frame(3)
	want := []string{"   ", "one"}

	if !slices.Equal(frame.Rows, want) {
		t.Errorf("expected leading whitespace preserved, got %q", frame.Rows)
	}
}

func TestAnOverlongWordStartsItsOwnRow(t *testing.T) {
	frame := (&Input{buffer: &buffer{runes: []rune("one abcdef")}}).Frame(5)
	want := []string{"one", "abcde", "f"}

	if !slices.Equal(frame.Rows, want) {
		t.Errorf("expected the word to start a row before the hard wrap, got %q", frame.Rows)
	}
}

func TestTheCursorCrossesAHiddenWrappingSpace(t *testing.T) {
	for cursor := 3; cursor <= 4; cursor++ {
		frame := (&Input{buffer: &buffer{runes: []rune("one two"), cursor: cursor}}).Frame(4)

		if frame.Row != 1 || frame.Column != 0 {
			t.Errorf("at %d expected the next row, got %d,%d", cursor, frame.Row, frame.Column)
		}
	}
}

func lines(count int) *Input {
	self := NewInput(nil)

	for number := range count {
		if number > 0 {
			self.Apply(key.Key{Code: key.Enter, Mod: key.Shift}, false)
		}

		self.Apply(key.Key{Code: key.Rune, Value: rune('a' + number%26)}, false)
	}

	return self
}

func TestWhatIsOutOfSightIsCountedAtBothEdges(t *testing.T) {
	self := lines(maxRows * 3)

	for range maxRows {
		self.Apply(key.Key{Code: key.Up}, false)
	}

	frame := self.Frame(80)

	if frame.Above == 0 || frame.Below == 0 {
		t.Errorf("expected rows out of sight both ways, got %d above and %d below",
			frame.Above, frame.Below)
	}

	if total := frame.Above + len(frame.Rows) + frame.Below; total != maxRows*3 {
		t.Errorf("expected the counts and the rows drawn to be the whole line, got %d", total)
	}
}

func TestNothingIsOutOfSightWhereTheLineFits(t *testing.T) {
	frame := lines(maxRows).Frame(80)

	if frame.Above != 0 || frame.Below != 0 {
		t.Errorf("expected nothing out of sight, got %d above and %d below",
			frame.Above, frame.Below)
	}
}

func TestAShortLineIsDrawnWhole(t *testing.T) {
	rows := lines(maxRows).Frame(80).Rows

	if len(rows) != maxRows {
		t.Errorf("expected all %d rows, got %d", maxRows, len(rows))
	}
}

func TestATallLineIsCutToTheTallestTheInputMayDraw(t *testing.T) {
	rows := lines(maxRows * 3).Frame(80).Rows

	if len(rows) != maxRows {
		t.Errorf("expected %d rows, got %d", maxRows, len(rows))
	}
}

func TestTheCursorStaysWithinTheRowsDrawn(t *testing.T) {
	self := lines(maxRows * 3)

	for moveCount := range maxRows * 3 {
		frame := self.Frame(80)
		rows, cursorRow := frame.Rows, frame.Row

		if cursorRow < 0 || cursorRow >= len(rows) {
			t.Fatalf("after %d moves the cursor sat at row %d of %d", moveCount, cursorRow, len(rows))
		}

		self.Apply(key.Key{Code: key.Up}, false)
	}
}

func TestTheEndOfATallLineIsWhatIsDrawn(t *testing.T) {
	frame := lines(maxRows + 2).Frame(80)
	rows, cursorRow := frame.Rows, frame.Row

	if want := string(rune('a' + (maxRows+1)%26)); rows[len(rows)-1] != want {
		t.Errorf("expected the last row to be %q, got %q", want, rows[len(rows)-1])
	}

	if cursorRow != maxRows-1 {
		t.Errorf("expected the cursor on the last row, got %d", cursorRow)
	}
}

func TestTheStartOfATallLineIsDrawnWhenTheCursorIsThere(t *testing.T) {
	self := lines(maxRows * 2)

	for range maxRows * 2 {
		self.Apply(key.Key{Code: key.Up}, false)
	}

	frame := self.Frame(80)
	rows, cursorRow := frame.Rows, frame.Row

	if rows[0] != "a" || cursorRow != 0 {
		t.Errorf("expected the first row drawn with the cursor on it, got %q at %d", rows[0], cursorRow)
	}
}

func TestACharacterWiderThanTheLineStaysWhereItIs(t *testing.T) {
	rows, cursorRow, cursorColumn := layout(&buffer{runes: []rune("日"), cursor: 1}, 1)

	if len(rows) != 2 || rows[0] != "日" {
		t.Errorf("expected the character on the first row, got %q", rows)
	}

	if cursorRow != 1 || cursorColumn != 0 {
		t.Errorf("expected the cursor on the next row, got %d,%d", cursorRow, cursorColumn)
	}
}

func TestControlCAndControlUAlwaysClearTheInput(t *testing.T) {
	for _, value := range []rune{'c', 'u'} {
		self := inputFromKeys(t, "hello")
		self.Apply(key.Key{Code: key.Rune, Value: 'x', Mod: key.Ctrl}, false)
		self.Apply(key.Key{Code: key.Rune, Value: value, Mod: key.Ctrl}, false)

		if self.Text() != "" || self.Pending() {
			t.Errorf("ctrl+%c left input %q with pending=%v", value, self.Text(), self.Pending())
		}
	}
}

func TestThePrefixAndALetterAskForOneSwap(t *testing.T) {
	for letter, want := range map[rune]Action{'w': Write, 'g': Git, 'b': Background} {
		self := NewInput(nil)

		self.Apply(key.Key{Code: key.Rune, Value: 'a'}, false)

		if got := self.Apply(key.Key{Code: key.Rune, Value: 'x', Mod: key.Ctrl}, false); got != Drawn {
			t.Errorf("ctrl+x: expected the prefix to swap nothing on its own, got %v", got)
		}

		if got := self.Apply(key.Key{Code: key.Rune, Value: letter}, false); got != want {
			t.Errorf("ctrl+x %c: expected %v, got %v", letter, want, got)
		}

		if got := self.Text(); got != "a" {
			t.Errorf("ctrl+x %c: expected the line to be left alone, got %q", letter, got)
		}
	}
}

func TestALetterNamingNoModeIsSwallowed(t *testing.T) {
	self := NewInput(nil)

	self.Apply(key.Key{Code: key.Rune, Value: 'x', Mod: key.Ctrl}, false)

	if got := self.Apply(key.Key{Code: key.Rune, Value: 'q'}, false); got != Drawn {
		t.Errorf("expected nothing to be asked for, got %v", got)
	}

	if got := self.Text(); got != "" {
		t.Errorf("expected the letter to be swallowed, got %q", got)
	}

	self.Apply(key.Key{Code: key.Rune, Value: 'w'}, false)

	if got := self.Text(); got != "w" {
		t.Errorf("expected the line to carry on as text, got %q", got)
	}
}

func TestControlDStopsARunningTurnWhateverIsTyped(t *testing.T) {
	keypress := key.Key{Code: key.Rune, Value: 'd', Mod: key.Ctrl}

	for name, inputText := range map[string]string{"empty": "", "typed": "hello"} {
		self := inputFromKeys(t, inputText)

		if got := self.Apply(keypress, true); got != Cancel {
			t.Errorf("%s: expected the turn to be cancelled, got %v", name, got)
		}

		if self.Text() != inputText {
			t.Errorf("%s: expected what was typed to be left alone, got %q", name, self.Text())
		}
	}
}

func TestControlDAtRestLeavesOnlyFromAnEmptyLine(t *testing.T) {
	keypress := key.Key{Code: key.Rune, Value: 'd', Mod: key.Ctrl}

	if got := inputFromKeys(t, "").Apply(keypress, false); got != Quit {
		t.Errorf("expected an empty line to be the way out, got %v", got)
	}

	self := inputFromKeys(t, "hello")
	self.Apply(key.Key{Code: key.Home}, false)

	if got := self.Apply(keypress, false); got == Quit {
		t.Errorf("expected a line with something on it to keep the harness, got %v", got)
	}

	if self.Text() != "hello" {
		t.Errorf("expected the line to be untouched, got %q", self.Text())
	}
}

func TestControlRAsksForTheHarnessToStartAgain(t *testing.T) {
	keypress := key.Key{Code: key.Rune, Value: 'r', Mod: key.Ctrl}

	if got := inputFromKeys(t, "hello").Apply(keypress, false); got != Restart {
		t.Errorf("expected a restart, got %v", got)
	}
}
