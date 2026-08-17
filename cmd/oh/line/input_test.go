package line

import (
	"bufio"
	"slices"
	"strings"
	"testing"

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

// Inside a paste a return is text; outside one it finishes the line.
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

func TestShiftReturnOpensALine(t *testing.T) {
	self := NewInput(nil)

	self.Apply(key.Key{Code: key.Rune, Value: 'a'}, false)
	self.Apply(key.Key{Code: key.Enter, Mod: key.Shift}, false)
	self.Apply(key.Key{Code: key.Rune, Value: 'b'}, false)

	if got := self.Text(); got != "a\nb" {
		t.Errorf("expected two lines, got %q", got)
	}
}

// A terminal lays out cells, so a row of two-cell characters holds half as many of them, and a
// character that would straddle the end goes on the next row whole.
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

// Leading whitespace is indentation, not a break, so it takes a row of its own rather than being
// swallowed the way the whitespace at a break is.
func TestLeadingWhitespaceKeepsItsRoom(t *testing.T) {
	frame := (&Input{buffer: &buffer{runes: []rune("    one")}}).Frame(3)
	want := []string{"   ", "one"}

	if !slices.Equal(frame.Rows, want) {
		t.Errorf("expected leading whitespace preserved, got %q", frame.Rows)
	}
}

// A word too wide for a row starts one of its own before it is cut, so the break falls between
// words wherever there is one to fall between.
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

// Both counts stand above zero at once where the cursor has text on either side of it, so neither
// edge may be worked out from the other.
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

// Scrolling is only worth anything if what is being typed is among what is drawn.
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

// The end of a long line is where the typing is, so that is what the window shows.
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

// Moving to the top of a long line scrolls it back, rather than leaving the cursor off the window.
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

// A character wider than the line cannot be made to fit, so the row it is on is the row it stays
// on, and the cursor never sits past the edge.
func TestACharacterWiderThanTheLineStaysWhereItIs(t *testing.T) {
	rows, cursorRow, cursorColumn := layout(&buffer{runes: []rune("日"), cursor: 1}, 1)

	if len(rows) != 2 || rows[0] != "日" {
		t.Errorf("expected the character on the first row, got %q", rows)
	}

	if cursorRow != 1 || cursorColumn != 0 {
		t.Errorf("expected the cursor on the next row, got %d,%d", cursorRow, cursorColumn)
	}
}

// Ctrl+x names no mode on its own: the letter after it does, and the pair leaves the line where it
// was. What becomes of the swap is the harness's business.
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

// A letter that names no mode is a slip, and a slip that typed a letter into the line would be a
// worse answer than one that did nothing at all.
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

// Ctrl+d asks for the turn to stop while one is running, and what has been typed has no say in
// that: a line half written is the next thing to send, not a reason to keep waiting on this one.
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

// With nothing running there is no turn to stop, and ctrl+d is the way out of an empty line. A line
// with something on it is neither, so nothing is what it comes to: a key that stops a turn one
// moment and takes a character out of a word the next is one nobody presses in a hurry.
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
