package lastTps

import (
	"fmt"

	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/style"
)

const threshold = 10

type state struct {
	rate          func() (tokensPerSecond float64, known bool)
	isTurnRunning func() bool
}

func New(
	rate func() (tokensPerSecond float64, known bool),
	isTurnRunning func() bool,
) segment.Factory {
	return func(segment.Options) (segment.Segment, error) {
		return state{rate: rate, isTurnRunning: isTurnRunning}, nil
	}
}

func (self state) Render(segment.Context) string {
	tokensPerSecond, known := self.rate()

	if !known {
		return style.Peripheral("?t/s")
	}

	format := "~%.0ft/s"
	if tokensPerSecond < threshold {
		format = "~%.1ft/s"
	}
	text := fmt.Sprintf(format, tokensPerSecond)
	if self.isTurnRunning() {
		return style.Peripheral(text)
	}

	return style.Normal(text)
}
