package main

import (
	"crdx.org/io/cmd/oh/painter"
	"crdx.org/io/cmd/oh/turn"
)

type Turn struct {
	*turn.Stream

	spinnerFrame int
	painter      *painter.Painter
}

type TurnEvent = turn.Event
