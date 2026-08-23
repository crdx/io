package turnElapsed

import (
	"time"

	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/internal/util"
)

var _ segment.Ticker = state{}

type state struct {
	turn func() (isRunning bool, took time.Duration)
}

func New(turn func() (isRunning bool, took time.Duration)) segment.Factory {
	return func(segment.Options) (segment.Segment, error) {
		return state{turn: turn}, nil
	}
}

func (self state) RefreshInterval() time.Duration {
	return time.Second
}

func (self state) Render(segment.Context) string {
	isRunning, took := self.turn()
	if !isRunning {
		return ""
	}

	return style.Subtle(util.CompactDuration(took.Truncate(time.Second)))
}
