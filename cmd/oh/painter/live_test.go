package painter

import (
	"strings"
	"testing"

	"crdx.org/io/cmd/oh/output"
)

func TestShortTextIsDrawnForEveryDeltaThatArrives(t *testing.T) {
	text := liveText{streamingMode: output.StreamingModePaced}

	for range liveTextFraction {
		text.Write("a")

		if !text.IsDue() {
			t.Fatalf("expected every delta of %d bytes to be drawn", text.Len())
		}

		text.MarkDrawn(false)
	}
}

func TestALongTextIsRedrawnOnceEveryStepAndNoMoreOften(t *testing.T) {
	text := liveText{streamingMode: output.StreamingModePaced}

	text.Write(strings.Repeat("a", 8*liveTextFraction))
	text.MarkDrawn(false)

	for range 7 {
		text.Write("a")

		if text.IsDue() {
			t.Fatalf("expected %d bytes to be too little to redraw at a length of %d", 7, text.Len())
		}
	}

	text.Write("a")

	if !text.IsDue() {
		t.Errorf("expected the eighth byte to be drawn at a length of %d", text.Len())
	}
}

func TestTheStepStopsGrowingSoThatALongAnswerStaysLive(t *testing.T) {
	text := liveText{streamingMode: output.StreamingModePaced}

	text.Write(strings.Repeat("a", 100*liveTextStepCap))
	text.MarkDrawn(false)

	if got := text.step(); got != liveTextStepCap {
		t.Errorf("step is %d bytes, want it capped at %d", got, liveTextStepCap)
	}
}

func TestTextThatArrivedWithoutBeingDrawnIsOwedUntilItIsTaken(t *testing.T) {
	text := liveText{streamingMode: output.StreamingModePaced}

	if text.IsOwed() {
		t.Error("expected nothing to be owed before anything arrives")
	}

	text.Write("some text")

	if !text.IsOwed() {
		t.Error("expected text that arrived to be owed")
	}

	text.MarkDrawn(false)

	if text.IsOwed() {
		t.Error("expected taken text to settle what was owed")
	}
}

func TestResettingForgetsWhatWasDrawnAsWellAsWhatArrived(t *testing.T) {
	text := liveText{streamingMode: output.StreamingModePaced}

	text.Write(strings.Repeat("a", 4*liveTextStepCap))
	text.MarkDrawn(false)
	text.Reset()

	if text.Len() != 0 || text.IsOwed() {
		t.Errorf("expected a reset to leave nothing arrived and nothing owed, got %d bytes", text.Len())
	}

	text.Write("a")

	if !text.IsDue() {
		t.Error("expected the first byte after a reset to be drawn")
	}
}
