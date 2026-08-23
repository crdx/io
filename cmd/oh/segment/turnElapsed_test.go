package segment_test

import (
	"testing"
	"time"

	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/segment/turnElapsed"
	"crdx.org/io/cmd/oh/style"
)

func turnLasting(t *testing.T, isRunning bool, took time.Duration) segment.Segment {
	t.Helper()

	built, err := turnElapsed.New(func() (bool, time.Duration) { return isRunning, took })(tomlOptions(""))
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
		built := turnLasting(t, true, took)

		if got := style.Plain(built.Render(segment.Context{})); got != want {
			t.Errorf("expected %s to read %q, got %q", took, want, got)
		}
	}
}

func TestTheElapsedSegmentSaysNothingBetweenTurns(t *testing.T) {
	built := turnLasting(t, false, time.Minute)

	if got := built.Render(segment.Context{}); got != "" {
		t.Errorf("expected an idle bar to leave the place empty, got %q", got)
	}
}

func TestTheElapsedSegmentAsksForASecondlyRedraw(t *testing.T) {
	layout := segment.Layout{segment.BottomLeft: {turnLasting(t, true, time.Second)}}

	if got := layout.RefreshInterval(); got != time.Second {
		t.Errorf("expected a redraw every second, got %s", got)
	}

	if got := layout.IdleRefreshInterval(); got != 0 {
		t.Errorf("expected nothing to be redrawn while no turn is running, got %s", got)
	}
}
