package turnTimer

import (
	"time"

	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/internal/util"
)

var _ segment.Persister = state{}

type state struct {
	timeElapsed func() time.Duration
}

func New(timeElapsed func() time.Duration) segment.Factory {
	return func(segment.Options) (segment.Segment, error) {
		return state{timeElapsed: timeElapsed}, nil
	}
}

func (self state) RefreshInterval() time.Duration {
	return time.Second
}

func (self state) Persistent() bool {
	return true
}

func (self state) Render(segment.Context) string {
	return style.Quantity(util.CompactDuration(self.timeElapsed().Truncate(time.Second)))
}
