package segment_test

import (
	"testing"
	"time"

	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/segment/turnElapsed"
	"crdx.org/io/cmd/oh/style"
)

func turnLasting(t *testing.T, isRunning bool, took time.Duration, known bool) segment.Segment {
	t.Helper()

	built, err := turnElapsed.New(func() (bool, time.Duration, bool) {
		return isRunning, took, known
	})(tomlOptions(""))
	if err != nil {
		t.Fatal(err)
	}

	return built
}

func TestTheElapsedSegmentCountsTheRunningTurnInWholeSeconds(t *testing.T) {
	for took, want := range map[time.Duration]string{
		900 * time.Millisecond:               "0s",
		9*time.Second + 400*time.Millisecond: "9s",
		69 * time.Second:                     "1m09s",
		2*time.Hour + 3*time.Minute:          "2h03m",
	} {
		built := turnLasting(t, true, took, true)

		if got := style.Plain(built.Render(segment.Context{})); got != want {
			t.Errorf("expected %s to read %q, got %q", took, want, got)
		}
	}
}

func TestTheElapsedSegmentKeepsTheCompletedTurnPeripheral(t *testing.T) {
	built := turnLasting(t, false, time.Minute, true)

	if got, want := built.Render(segment.Context{}), style.Peripheral("1m00s"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTheElapsedSegmentShowsAnUnknownDurationBeforeTheFirstTurn(t *testing.T) {
	built := turnLasting(t, false, 0, false)

	if got, want := built.Render(segment.Context{}), style.Peripheral("?s"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTheElapsedSegmentAsksForASecondlyRedraw(t *testing.T) {
	layout := segment.Layout{segment.BottomLeft: {turnLasting(t, true, time.Second, true)}}

	if got := layout.RefreshInterval(); got != time.Second {
		t.Errorf("expected a redraw every second, got %s", got)
	}

	if got := layout.IdleRefreshInterval(); got != 0 {
		t.Errorf("expected nothing to be redrawn while no turn is running, got %s", got)
	}
}
