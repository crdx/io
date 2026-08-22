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

	events        chan TurnEvent
	pendingEvents agent.Coalescer
}

type QueuedTurn struct {
	nextMessage   string
	isReplacement bool
	isModeChange  bool
}

type TurnEvent struct {
	event agent.Event
	err   error
}
