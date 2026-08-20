package main

import (
	"context"

	"crdx.org/io/agent"
)

type turn struct {
	isRunning bool
	frame     int // the banner spinner frame

	isCancelled bool               // whether the user stopped it
	stop        context.CancelFunc // stops the provider request

	events        chan turnEvent // events arriving from the agent
	painter       *painter
	pendingEvents agent.Coalescer // events held until they make a complete journal record
	failure       error           // why the turn failed
}

type queuedTurn struct {
	prompt        string // what to ask as soon as an interrupted turn finishes
	isReplacement bool   // whether an interrupted turn has a replacement
	isModeChange  bool   // whether changed capabilities should restart an interrupted turn
}

type turnEvent struct {
	event agent.Event
	err   error // why the stream ended
}
