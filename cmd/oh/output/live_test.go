package output

import (
	"fmt"
	"strings"
	"testing"
)

func region() (*Output, *strings.Builder) {
	screenOutput := &strings.Builder{}

	return &Output{writer: screenOutput, terminal: true, columns: 40, lines: 24}, screenOutput
}

// A delta usually changes the last row and nothing else, so that is all that is painted again. The
// cost of a long answer is one row per delta rather than the whole of it.
func TestOnlyTheRowsThatChangedArePaintedAgain(t *testing.T) {
	screen, screenOutput := region()

	screen.Draw([]string{"one", "two", "three"})
	screenOutput.Reset()

	screen.Draw([]string{"one", "two", "three!"})

	got := screenOutput.String()

	if strings.Contains(got, "one") || strings.Contains(got, "two") {
		t.Errorf("expected the rows above the difference to be left alone, got %q", got)
	}

	if !strings.Contains(got, "three!") {
		t.Errorf("expected the row that changed to be drawn, got %q", got)
	}
}

func TestARowAddedBelowTheRestOpensARowOfItsOwn(t *testing.T) {
	screen, screenOutput := region()

	screen.Draw([]string{"one", "two"})
	screenOutput.Reset()

	screen.Draw([]string{"one", "two", "three"})

	if want := "\r\n\r" + clearRow + "three"; !strings.Contains(screenOutput.String(), want) {
		t.Errorf("expected the new row on a row of its own, got %q", screenOutput.String())
	}
}

func TestARowRewrittenHigherUpIsReachedByMovingBackToIt(t *testing.T) {
	screen, screenOutput := region()

	screen.Draw([]string{"one", "two", "three"})
	screenOutput.Reset()

	screen.Draw([]string{"one", "TWO", "three"})

	got := screenOutput.String()

	if want := fmt.Sprintf(up, 1); !strings.Contains(got, want) {
		t.Errorf("expected the cursor to move back a row, got %q", got)
	}

	if !strings.Contains(got, "TWO") || !strings.Contains(got, "three") {
		t.Errorf("expected the row and everything under it to be drawn again, got %q", got)
	}
}

// A terminal cannot move the cursor into its history, so a difference above what the screen still
// holds is not a repair anyone can make. Saying so is the whole of the answer: the caller clears
// and says the conversation again.
func TestADifferenceAboveTheScreenIsReportedRatherThanRepaired(t *testing.T) {
	screen, _ := region()

	screen.lines = 4

	rows := []string{"one", "two", "three", "four", "five", "six"}

	if !screen.Draw(rows) {
		t.Fatal("expected the first drawing to be made")
	}

	if screen.top != len(rows)-4 {
		t.Fatalf("expected %d rows to have scrolled off, got %d", len(rows)-4, screen.top)
	}

	if screen.Draw([]string{"ONE", "two", "three", "four", "five", "six"}) {
		t.Error("expected a difference above the screen to be reported")
	}

	if !screen.Draw([]string{"one", "two", "three", "four", "five", "SIX"}) {
		t.Error("expected a difference on the screen to be repaired")
	}
}

// The region is the turn in progress and nothing else. Anything written outside it ends it, so what
// follows starts on a row of its own rather than over the top of the answer.
func TestWritingOutsideTheRegionEndsIt(t *testing.T) {
	screen, _ := region()

	screen.Draw([]string{"one", "two"})
	screen.Line("a line")

	if screen.liveRows != nil {
		t.Errorf("expected the region to be forgotten, got %q", screen.liveRows)
	}

	screen.Draw([]string{"three"})

	if len(screen.liveRows) != 1 {
		t.Errorf("expected a region of its own, got %q", screen.liveRows)
	}
}

// A row is only settled once nothing further can change it, and markdown offers no such row: the
// third character of a fence turns the two before it from text into a block. With no terminal to
// take a row back with, the answer is written once it is whole.
func TestWithoutATerminalTheAnswerIsWrittenOnceItIsWhole(t *testing.T) {
	screenOutput := &strings.Builder{}

	screen := New(screenOutput)

	screen.Draw([]string{"one"})
	screen.Draw([]string{"one", "two"})

	if got := screenOutput.String(); got != "" {
		t.Errorf("expected the answer to be held back until it was whole, got %q", got)
	}

	screen.End()

	if got := screenOutput.String(); got != "one\ntwo\n" {
		t.Errorf("expected the whole answer on the way out, got %q", got)
	}
}

// Markdown may become shorter as a delimiter arrives. Rows still on screen are removed in place
// rather than making the caller clear and replay the conversation.
func TestFewerRowsThanBeforeAreRepaired(t *testing.T) {
	screen, screenOutput := region()

	screen.Draw([]string{"one", "two", "three"})
	screenOutput.Reset()

	if !screen.Draw([]string{"one", "two"}) {
		t.Fatal("expected a shorter set of rows to be repaired")
	}

	if !strings.Contains(screenOutput.String(), clearBelow) {
		t.Errorf("expected the surplus rows to be cleared, got %q", screenOutput.String())
	}
}

// The third character of a fence takes the two before it off the screen. The open region keeps an
// empty row for whatever the fence says next.
func TestAFrameWithNoRowsErasesWhatWasDrawn(t *testing.T) {
	screen, screenOutput := region()

	if !screen.Draw(nil) {
		t.Error("expected nothing drawn against nothing to be no trouble")
	}

	screen.Draw([]string{"``"})
	screenOutput.Reset()

	if !screen.Draw(nil) {
		t.Fatal("expected an empty frame to be repaired")
	}

	if !strings.Contains(screenOutput.String(), clearRow) {
		t.Errorf("expected the old row to be cleared, got %q", screenOutput.String())
	}
}
