package scrollOverflow

import (
	"fmt"

	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/style"
)

const (
	up   = "up"
	down = "down"
)

type state struct {
	arrow string
	up    bool
}

func New(options segment.Options) (segment.Segment, error) {
	var args struct {
		Direction string `toml:"direction"`
	}

	if err := options.Read(&args); err != nil {
		return nil, err
	}

	switch args.Direction {
	case up:
		return state{arrow: "↑", up: true}, nil
	case down:
		return state{arrow: "↓"}, nil
	default:
		return nil, fmt.Errorf("direction is %q, and wants to be %s or %s", args.Direction, up, down)
	}
}

func (self state) Render(context segment.Context) string {
	rows := context.HiddenLinesBelow
	if self.up {
		rows = context.HiddenLinesAbove
	}

	if rows == 0 {
		return ""
	}

	return style.ScrolledInput(fmt.Sprintf("%s %d", self.arrow, rows))
}
