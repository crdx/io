package main

import (
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"crdx.org/io/agent"
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

func TestTheBarCountsATurnForEveryThingAsked(t *testing.T) {
	held := &Harness{}

	for range 3 {
		held.countTowardsTheBar(agent.Event{Kind: agent.UserMessageEvent, Text: "go on then"})
		held.countTowardsTheBar(agent.Event{Kind: agent.ModelMessageEvent, Text: "right you are"})
	}

	if got := held.turnCount(); got != 3 {
		t.Errorf("expected three turns, got %d", got)
	}
}

func TestTheBarLearnsHowFastTheTurnThatJustEndedCameBack(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		held := &Harness{}
		held.currentTurn.startedAt = time.Now()

		held.countTowardsTheBar(agent.Event{
			Kind: agent.ModelReasoningEvent,
			Text: strings.Repeat("x", 200),
		})
		held.countTowardsTheBar(agent.Event{
			Kind: agent.ModelMessageEvent,
			Text: strings.Repeat("x", 200),
		})

		time.Sleep(10 * time.Second)
		held.recordTokenRate()

		tokensPerSecond, known := held.lastTurnTokenRate()
		if !known || tokensPerSecond != 10 {
			t.Errorf("expected ten tokens a second, got %v (known: %v)", tokensPerSecond, known)
		}
	})
}

func TestABarWithNothingStreamedKeepsTheRateItHad(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		held := &Harness{lastTurnRate: 12}
		held.currentTurn.startedAt = time.Now()

		time.Sleep(time.Second)
		held.recordTokenRate()

		if tokensPerSecond, _ := held.lastTurnTokenRate(); tokensPerSecond != 12 {
			t.Errorf("expected a turn that said nothing to leave the rate alone, got %v", tokensPerSecond)
		}
	})
}
