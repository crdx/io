package interaction

import (
	"os"
	"testing"
	"testing/synctest"
	"time"

	"crdx.org/io/cmd/oh/key"
	"crdx.org/io/cmd/oh/turn"
)

func TestABarWithNothingToSayIsNeverRedrawn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		refresh := newRefreshTimer(func(time.Time) time.Time { return time.Time{} })
		defer refresh.stop()

		refresh.schedule()

		time.Sleep(time.Hour)

		select {
		case at := <-refresh.timer.C:
			t.Errorf("expected a still bar to be left alone, got a redraw at %s", at)
		default:
		}
	})
}

func TestARefreshIsTakenAtTheMomentItWasAskedFor(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		startedAt := time.Now()

		refresh := newRefreshTimer(func(at time.Time) time.Time {
			return at.Truncate(time.Second).Add(time.Second)
		})
		defer refresh.stop()

		refresh.schedule()

		at := <-refresh.timer.C

		if got := at.Sub(startedAt); got != time.Second {
			t.Errorf("expected the redraw a second on, got it %s on", got)
		}
	})
}

func TestALateRescheduleKeepsTheMomentAlreadyDueRatherThanPushingItAway(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		startedAt := time.Now()
		dueAt := startedAt.Add(time.Second)

		refresh := newRefreshTimer(func(time.Time) time.Time { return dueAt })
		defer refresh.stop()

		for range 8 {
			refresh.schedule()
			time.Sleep(100 * time.Millisecond)
		}

		at := <-refresh.timer.C

		if got := at.Sub(startedAt); got != time.Second {
			t.Errorf("expected the redraw to stand at a second on, got it %s on", got)
		}
	})
}

func TestARefreshAlreadyGoneIsTakenAtOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		startedAt := time.Now()

		refresh := newRefreshTimer(func(at time.Time) time.Time { return at.Add(-time.Minute) })
		defer refresh.stop()

		refresh.schedule()

		at := <-refresh.timer.C

		if got := at.Sub(startedAt); got != soonest {
			t.Errorf("expected the redraw to be put off by %s, got %s", soonest, got)
		}
	})
}

func TestABarThatSettlesStopsBeingRedrawn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dueAt := time.Now().Add(time.Second)

		refresh := newRefreshTimer(func(at time.Time) time.Time {
			if at.After(dueAt) {
				return time.Time{}
			}

			return dueAt
		})
		defer refresh.stop()

		refresh.schedule()
		<-refresh.timer.C

		time.Sleep(time.Millisecond)
		refresh.schedule()
		time.Sleep(time.Hour)

		select {
		case at := <-refresh.timer.C:
			t.Errorf("expected the bar to settle, got a redraw at %s", at)
		default:
		}
	})
}

func TestRunStopsAndRedraws(t *testing.T) {
	t.Run("key", func(t *testing.T) {
		keys := make(chan key.Key, 1)
		keys <- key.Key{Code: key.Escape}
		handled := false
		run(keys, make(chan os.Signal), make(chan time.Time), func() {}, nil, Handler{Events: func() <-chan turn.Event { return make(chan turn.Event) }, Key: func(key.Key) bool { handled = true; return false }})
		if !handled {
			t.Error("key not handled")
		}
	})
	t.Run("finished turn requests exit", func(t *testing.T) {
		turnEvents := make(chan turn.Event)
		close(turnEvents)
		finished := false
		run(make(chan key.Key), make(chan os.Signal), make(chan time.Time), func() {}, nil, Handler{
			Events: func() <-chan turn.Event { return turnEvents },
			TurnFinished: func() bool {
				finished = true
				return false
			},
		})
		if !finished {
			t.Error("finished turn was not handled")
		}
	})
	t.Run("refresh", func(t *testing.T) {
		keys := make(chan key.Key)
		refreshes := make(chan time.Time, 1)
		refreshes <- time.Now()
		scheduled, drawn := 0, false
		run(keys, make(chan os.Signal), refreshes, func() { scheduled++ }, nil, Handler{Events: func() <-chan turn.Event { return make(chan turn.Event) }, Draw: func() { drawn = true; close(keys) }, Key: func(key.Key) bool { return false }})
		if scheduled < 2 || !drawn {
			t.Errorf("scheduled=%d drawn=%t", scheduled, drawn)
		}
	})
	t.Run("ignored change", func(t *testing.T) {
		keys := make(chan key.Key)
		changes := make(chan error, 1)
		changes <- nil
		drawn := false
		run(keys, make(chan os.Signal), make(chan time.Time), func() {}, nil, Handler{
			Events:  func() <-chan turn.Event { return make(chan turn.Event) },
			Key:     func(key.Key) bool { return false },
			Changes: changes,
			Change: func(error) bool {
				close(keys)
				return false
			},
			Draw: func() { drawn = true },
		})
		if drawn {
			t.Error("ignored change was drawn")
		}
	})
	t.Run("heartbeat with a redraw already due", func(t *testing.T) {
		keys := make(chan key.Key)
		heartbeats := make(chan time.Time, 1)
		heartbeats <- time.Now()
		refreshes := make(chan time.Time, 1)
		beaten, drawn := false, false
		run(keys, make(chan os.Signal), refreshes, func() {}, heartbeats, Handler{
			Events: func() <-chan turn.Event { return make(chan turn.Event) },
			Beat:   func() { beaten = true; refreshes <- time.Now() },
			Key:    func(key.Key) bool { return false },
			Draw:   func() { drawn = true; close(keys) },
		})
		if !beaten || !drawn {
			t.Errorf("beaten=%t drawn=%t", beaten, drawn)
		}
	})
	t.Run("heartbeat", func(t *testing.T) {
		keys := make(chan key.Key)
		heartbeats := make(chan time.Time, 1)
		heartbeats <- time.Now()
		beaten, drawn := false, false
		run(keys, make(chan os.Signal), make(chan time.Time), func() {}, heartbeats, Handler{
			Events: func() <-chan turn.Event { return make(chan turn.Event) },
			Beat:   func() { beaten = true; close(keys) },
			Key:    func(key.Key) bool { return false },
			Draw:   func() { drawn = true },
		})
		if !beaten || drawn {
			t.Errorf("beaten=%t drawn=%t", beaten, drawn)
		}
	})
}
