package interaction

import (
	"os"
	"testing"
	"testing/synctest"
	"time"

	"crdx.org/io/cmd/oh/key"
	"crdx.org/io/cmd/oh/turn"
)

func TestABarWithNothingIdlingIsNeverRedrawnBetweenTurns(t *testing.T) {
	idle := idleRefresh{getInterval: func() time.Duration { return 0 }}

	if idle.isDue() {
		t.Error("expected a still bar to be left alone")
	}
}

func TestAnIdlingBarIsRedrawnAtItsOwnPaceNotTheTickers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		idle := idleRefresh{getInterval: func() time.Duration { return time.Second }}

		if !idle.isDue() {
			t.Fatal("expected the first idle tick to draw")
		}

		time.Sleep(125 * time.Millisecond)

		if idle.isDue() {
			t.Error("expected a tick sooner than the interval to be passed over")
		}

		time.Sleep(time.Second)

		if !idle.isDue() {
			t.Error("expected the tick after the interval to draw")
		}
	})
}

func TestAnIdlingBarCanChangeItsRedrawPace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		interval := time.Second
		idle := idleRefresh{getInterval: func() time.Duration { return interval }}

		if !idle.isDue() {
			t.Fatal("expected the first idle tick to draw")
		}

		time.Sleep(125 * time.Millisecond)
		interval = 100 * time.Millisecond

		if !idle.isDue() {
			t.Error("expected the newly shortened interval to take effect")
		}
	})
}

func TestRunStopsAndTicks(t *testing.T) {
	t.Run("key", func(t *testing.T) {
		keys := make(chan key.Key, 1)
		keys <- key.Key{Code: key.Escape}
		handled := false
		run(keys, make(chan os.Signal), make(chan time.Time), func() time.Duration { return 0 }, Handler{Events: func() <-chan turn.Event { return make(chan turn.Event) }, Key: func(key.Key) bool { handled = true; return false }})
		if !handled {
			t.Error("key not handled")
		}
	})
	t.Run("finished turn requests exit", func(t *testing.T) {
		turnEvents := make(chan turn.Event)
		close(turnEvents)
		finished := false
		run(make(chan key.Key), make(chan os.Signal), make(chan time.Time), func() time.Duration { return 0 }, Handler{
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
	t.Run("tick", func(t *testing.T) {
		keys := make(chan key.Key)
		ticks := make(chan time.Time, 1)
		ticks <- time.Now()
		ticked, drawn := false, false
		run(keys, make(chan os.Signal), ticks, func() time.Duration { return 0 }, Handler{Events: func() <-chan turn.Event { return make(chan turn.Event) }, Running: func() bool { return true }, Tick: func() { ticked = true; close(keys) }, Draw: func() { drawn = true }})
		if !ticked || !drawn {
			t.Errorf("ticked=%t drawn=%t", ticked, drawn)
		}
	})
}
