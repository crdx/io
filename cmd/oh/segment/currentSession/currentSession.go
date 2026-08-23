package currentSession

import (
	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/style"
)

type state struct {
	name string
}

func New(name string) segment.Factory {
	return func(segment.Options) (segment.Segment, error) {
		return state{name: name}, nil
	}
}

func (self state) Render(segment.Context) string {
	return style.Subtle(self.name)
}
