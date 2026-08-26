package output

import (
	"slices"
	"strings"
	"testing"
)

type mutableBlock struct {
	text string
}

func (self *mutableBlock) Rows(_ int) []string {
	return []string{self.text}
}

func TestARefreshInsideASynchronisedUpdateIsDrawnOnceAtItsClose(t *testing.T) {
	screen, screenOutput := region()
	block := &mutableBlock{text: "first"}

	screen.Sync(func() {
		screen.Open(block)
		block.text = "second"
		screen.Refresh()
		block.text = "third"
		screen.Refresh()
	})

	drawn := screenOutput.String()

	for _, unseen := range []string{"first", "second"} {
		if strings.Contains(drawn, unseen) {
			t.Errorf("an unseen state of the block was drawn: %q in %q", unseen, drawn)
		}
	}
	if count := strings.Count(drawn, "third"); count != 1 {
		t.Errorf("the last state of the block was drawn %d times, want once: %q", count, drawn)
	}
}

func TestSealingInsideASynchronisedUpdateDrawsTheRefreshItOwes(t *testing.T) {
	screen, screenOutput := region()
	block := &mutableBlock{text: "running"}

	screen.Sync(func() {
		screen.Open(block)
		block.text = "finished"
		screen.Refresh()
		screen.Seal()
	})

	drawn := screenOutput.String()

	if !strings.Contains(drawn, "finished") {
		t.Errorf("the sealed block lost the refresh it was owed: %q", drawn)
	}
	if strings.Contains(drawn, "running") {
		t.Errorf("a superseded state of the block was drawn: %q", drawn)
	}
}

func TestDiscardingANoticeBlockRestoresTheDrawingOrigin(t *testing.T) {
	screen, _ := screenWithInput()
	before := screen.drawingState()
	beforeInput := screen.input

	handle := screen.OpenNotice(textBlock{text: "temporary notice"})
	if !screen.DiscardBlock(handle) {
		t.Fatal("expected the notice block to remain retractable")
	}

	if got := screen.drawingState(); got != before {
		t.Errorf("drawing state was not restored: got %+v, want %+v", got, before)
	}
	if !slices.Equal(screen.shownFooter.rows, beforeInput.rows) || screen.shownFooter.cursorRow != beforeInput.cursorRow || screen.shownFooter.cursorColumn != beforeInput.cursorColumn {
		t.Errorf("input was not restored: got %+v, want %+v", screen.shownFooter, beforeInput)
	}
}

func TestAnOldBlockHandleCannotDiscardANewerBlock(t *testing.T) {
	screen, _ := region()

	oldHandle := screen.OpenNotice(textBlock{text: "old"})
	screen.Seal()
	newHandle := screen.OpenNotice(textBlock{text: "new"})

	if screen.DiscardBlock(oldHandle) {
		t.Error("an old handle discarded a newer block")
	}
	if !screen.DiscardBlock(newHandle) {
		t.Error("the current block could not be discarded")
	}
}
