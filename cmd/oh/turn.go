package main

import (
	"context"

	"crdx.org/io/agent"
)

type turn struct {
	running bool // whether a turn is underway
	frame   int  // the banner spinner frame

	cancelled bool               // whether the user stopped it
	stop      context.CancelFunc // stops the provider request

	events        chan turnEvent  // events arriving from the agent
	painter       *painter        // the turn's display
	pendingEvents agent.Coalescer // events held until they make a complete journal record
	failure       error           // why the turn failed
}

type turnEvent struct {
	event agent.Event // what happened
	err   error       // why the stream ended
}
