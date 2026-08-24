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

	spinnerFrame int
	painter      *Painter
	startedAt    time.Time

	events chan TurnEvent
}

type TurnEvent struct {
	update agent.Update
	err    error
}
