package activeModel

import (
	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/style"
)

const separator = " ─ "

type state struct {
	name   string
	effort string
}

func New(name string, effort string) segment.Factory {
	return func(segment.Options) (segment.Segment, error) {
		return state{name: name, effort: short(effort)}, nil
	}
}

func short(level string) string {
	switch level {
	case "minimal":
		return "min"
	case "medium":
		return "med"
	default:
		return level
	}
}

func (self state) Render(segment.Context) string {
	return style.Subtle(self.name + separator + self.effort)
}
