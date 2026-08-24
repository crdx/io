package main

import (
	"testing"
	"time"
)

func TestTurnElapsedKeepsTheCompletedTurnDuration(t *testing.T) {
	startedAt := time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC)
	harness := Harness{currentTurn: Turn{
		Stream: testTimedTurnStream(false, startedAt, startedAt.Add(69*time.Second)),
	}}

	isRunning, took, known := harness.turnElapsed()
	if isRunning || !known || took != 69*time.Second {
		t.Errorf("got running=%v, took=%s, known=%v", isRunning, took, known)
	}
}

func TestTurnElapsedIsUnknownBeforeTheFirstTurn(t *testing.T) {
	isRunning, took, known := (&Harness{}).turnElapsed()
	if isRunning || known || took != 0 {
		t.Errorf("got running=%v, took=%s, known=%v", isRunning, took, known)
	}
}
