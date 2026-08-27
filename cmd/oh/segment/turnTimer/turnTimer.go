package turnTimer

import (
	"time"

	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/internal/util"
)

var _ segment.Refresher = state{}

const step = time.Second

type state struct {
	timeElapsed func() time.Duration
}

func New(timeElapsed func() time.Duration) segment.Factory {
	return func(segment.Options) (segment.Segment, error) {
		return state{timeElapsed: timeElapsed}, nil
	}
}

func (self state) NextRefresh(phase segment.Phase) time.Time {
	return phase.At.Add(step - self.timeElapsed()%step)
}

func (self state) Render(segment.Context) string {
	return style.Quantity(util.CompactDuration(self.timeElapsed().Truncate(step)))
}
