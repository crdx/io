package turnElapsed

import (
	"time"

	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/internal/util"
)

var _ segment.Ticker = state{}

type state struct {
	turn func() (isRunning bool, took time.Duration, known bool)
}

func New(turn func() (isRunning bool, took time.Duration, known bool)) segment.Factory {
	return func(segment.Options) (segment.Segment, error) {
		return state{turn: turn}, nil
	}
}

func (self state) RefreshInterval() time.Duration {
	return time.Second
}

func (self state) Render(segment.Context) string {
	isRunning, took, known := self.turn()

	text := "?s"
	if known {
		text = util.CompactDuration(took.Truncate(time.Second))
	}
	if isRunning {
		return style.Normal(text)
	}

	return style.Peripheral(text)
}
