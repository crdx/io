package lastTps

import (
	"fmt"

	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/style"
)

const threshold = 10

type state struct {
	rate func() (tokensPerSecond float64, known bool)
}

func New(rate func() (tokensPerSecond float64, known bool)) segment.Factory {
	return func(segment.Options) (segment.Segment, error) {
		return state{rate: rate}, nil
	}
}

func (self state) Render(segment.Context) string {
	tps, known := self.rate()
	if !known {
		return ""
	}

	if tps < threshold {
		return style.Subtle(fmt.Sprintf("~%.1ft/s", tps))
	}

	return style.Subtle(fmt.Sprintf("~%.0ft/s", tps))
}
