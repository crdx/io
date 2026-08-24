package main

import (
	"context"
	"time"

	"crdx.org/io/cmd/oh/turn"
)

func testRunningTurnStream() *turn.Stream {
	return testTurnStream(nil, nil, turn.State{Running: true})
}

func testTurnStreamForRunning(isRunning bool) *turn.Stream {
	if !isRunning {
		return nil
	}
	return testRunningTurnStream()
}

func testTimedTurnStream(isRunning bool, startedAt time.Time, finishedAt time.Time) *turn.Stream {
	if isRunning {
		finishedAt = time.Time{}
	}
	return testTurnStream(nil, nil, turn.State{Running: isRunning, StartedAt: startedAt, FinishedAt: finishedAt})
}

func testRunningTurnStreamWithCancel(cancel context.CancelFunc) *turn.Stream {
	return testTurnStream(nil, cancel, turn.State{Running: true})
}

func testTurnStream(events chan TurnEvent, cancel context.CancelFunc, state turn.State) *turn.Stream {
	if events == nil {
		events = make(chan TurnEvent)
	}
	if cancel == nil {
		cancel = func() {}
	}
	if state.StartedAt.IsZero() {
		state.StartedAt = time.Now()
	}
	return turn.Adopt(events, cancel, state)
}
