package segment_test

import (
	"testing"
	"time"

	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/segment/localTime"
	"crdx.org/io/cmd/oh/style"
)

func stoppedClock(t *testing.T, options segment.Options) segment.Segment {
	t.Helper()

	at := time.Date(2026, time.August, 17, 14, 32, 9, 0, time.UTC)

	built, err := localTime.New(func() time.Time { return at })(options)
	if err != nil {
		t.Fatal(err)
	}

	return built
}

func TestTheClockSegmentTellsTheTimeToTheMinuteByDefault(t *testing.T) {
	built := stoppedClock(t, tomlOptions(""))

	if got := style.Plain(built.Render(segment.Context{})); got != "14:32" {
		t.Errorf("expected the default format to stop at the minute, got %q", got)
	}
}

func TestTheClockSegmentTakesTheFormatItIsGiven(t *testing.T) {
	built := stoppedClock(t, tomlOptions("format = \"15:04:05\"\n"))

	if got := style.Plain(built.Render(segment.Context{})); got != "14:32:09" {
		t.Errorf("expected the given format to be honoured, got %q", got)
	}
}

func TestTheClockSegmentKeepsTheBarTickingBetweenTurns(t *testing.T) {
	layout := segment.Layout{segment.TopRight: {stoppedClock(t, tomlOptions(""))}}

	if got := layout.RefreshInterval(); got != time.Second {
		t.Errorf("expected a redraw every second, got %s", got)
	}

	if got := layout.IdleRefreshInterval(); got != time.Second {
		t.Errorf("expected a clock to be redrawn every second between turns, got %s", got)
	}
}

func TestALayoutWithNothingIdlingSaysSo(t *testing.T) {
	layout := segment.Layout{segment.TopRight: {offeringSegment(t, "gpt")}}

	if got := layout.IdleRefreshInterval(); got != 0 {
		t.Errorf("expected a still bar to be left alone between turns, got %s", got)
	}
}

func offeringSegment(t *testing.T, text string) segment.Segment {
	t.Helper()

	built, err := offering(text)(tomlOptions(""))
	if err != nil {
		t.Fatal(err)
	}

	return built
}
