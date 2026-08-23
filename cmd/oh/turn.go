package main

import (
	"context"

	"crdx.org/io/agent"
)

type Turn struct {
	isRunning   bool
	isCancelled bool
	cancel      context.CancelFunc
	err         error

	spinnerFrame int
	painter      *Painter

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
