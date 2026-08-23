package activeModel

import (
	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/style"
)

type state struct {
	name   string
	effort string
}

func New(name string, effort string) segment.Factory {
	return func(segment.Options) (segment.Segment, error) {
		return state{name: name, effort: effort}, nil
	}
}

func (self state) Render(segment.Context) string {
	return style.Subtle(self.effort + " " + self.name)
}
