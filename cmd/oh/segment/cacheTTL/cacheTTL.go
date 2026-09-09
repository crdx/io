package cacheTTL

import (
	"time"

	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/internal/util"
)

type state struct {
	lifetime func() time.Duration
}

func New(lifetime func() time.Duration) segment.Factory {
	return func(options segment.Options) (segment.Segment, error) {
		if err := options.Read(&struct{}{}); err != nil {
			return nil, err
		}

		return state{lifetime: lifetime}, nil
	}
}

func (self state) Render(segment.Context) string {
	lifetime := self.lifetime()
	if lifetime <= 0 {
		return ""
	}

	return style.Information(util.CompactDuration(lifetime)) + " " + style.Subtle("ttl")
}
