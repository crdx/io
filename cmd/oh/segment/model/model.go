package model

import (
	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/style"
)

type state struct {
	shown string
}

func New(name string) segment.Factory {
	return func(segment.Unmarshall) (segment.Instance, error) {
		return state{shown: name}, nil
	}
}

func (self state) Render(segment.Context) string {
	return style.Subtle(self.shown)
}
