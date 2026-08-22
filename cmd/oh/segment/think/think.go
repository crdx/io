// Package think is how hard the model was asked to think, shortened to the room a rule has for it.
package think

import (
	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/style"
)

type state struct {
	value string
}

func New(level string) segment.Factory {
	return func(segment.Unmarshall) (segment.Instance, error) {
		return state{value: short(level)}, nil
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
	return style.Subtle(self.value)
}
