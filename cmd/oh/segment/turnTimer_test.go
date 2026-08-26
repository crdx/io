package segment_test

import (
	"testing"
	"time"

	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/segment/turnTimer"
	"crdx.org/io/cmd/oh/style"
)

func timerShowing(t *testing.T, elapsed time.Duration) segment.Segment {
	t.Helper()

	built, err := turnTimer.New(func() time.Duration {
		return elapsed
	})(tomlOptions(""))
	if err != nil {
		t.Fatal(err)
	}

	return built
}

func TestTheTurnTimerCountsInWholeSeconds(t *testing.T) {
	for elapsed, want := range map[time.Duration]string{
		900 * time.Millisecond:               "0s",
		9*time.Second + 400*time.Millisecond: "9s",
		69 * time.Second:                     "1m09s",
		2*time.Hour + 3*time.Minute:          "2h03m",
	} {
		built := timerShowing(t, elapsed)

		if got := style.Plain(built.Render(segment.Context{})); got != want {
			t.Errorf("expected %s to read %q, got %q", elapsed, want, got)
		}
	}
}

func TestTheTurnTimerHoldsItsUnitsBackFromItsNumbers(t *testing.T) {
	built := timerShowing(t, 69*time.Second)

	if got, want := built.Render(segment.Context{}), style.Quantity("1m09s"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTheTurnTimerAsksForASecondlyRedraw(t *testing.T) {
	layout := segment.Layout{segment.BottomLeft: {timerShowing(t, time.Second)}}

	if got := layout.RefreshInterval(); got != time.Second {
		t.Errorf("expected a redraw every second, got %s", got)
	}

	if got := layout.IdleRefreshInterval(); got != time.Second {
		t.Errorf("expected a redraw every second while idle, got %s", got)
	}
}
