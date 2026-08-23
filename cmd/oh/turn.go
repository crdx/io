package main

import (
	"context"
	"time"

	"crdx.org/io/agent"
)

type Turn struct {
	isRunning   bool
	isCancelled bool
	cancel      context.CancelFunc
	err         error

	spinnerFrame  int
	painter       *Painter
	startedAt     time.Time
	streamedBytes int

	events chan TurnEvent
}

type QueuedTurn struct {
	nextMessage   string
	isReplacement bool
	isModeChange  bool
}

type TurnEvent struct {
	update agent.Update
	err    error
}
