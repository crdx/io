package main

import (
	"testing"
	"testing/synctest"
	"time"
)

func TestABarWithNothingIdlingIsNeverRedrawnBetweenTurns(t *testing.T) {
	idle := idleRefresh{}

	if idle.isDue() {
		t.Error("expected a still bar to be left alone")
	}
}

func TestAnIdlingBarIsRedrawnAtItsOwnPaceNotTheTickers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		idle := idleRefresh{interval: time.Second}

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
