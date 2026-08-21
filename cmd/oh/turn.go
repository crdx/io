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
	nextMessage   string // what to ask as soon as an interrupted turn finishes
	isReplacement bool   // whether an interrupted turn has a replacement
	isModeChange  bool   // whether changed capabilities should restart an interrupted turn
}

type TurnEvent struct {
	event agent.Event
	err   error
}
