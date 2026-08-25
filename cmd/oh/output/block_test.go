package output

import (
	"slices"
	"testing"
)

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
