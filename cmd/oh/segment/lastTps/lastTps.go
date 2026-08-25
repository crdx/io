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

	text := "?tps"
	if known {
		format := "~%.0ftps"
		if tokensPerSecond < threshold {
			format = "~%.1ftps"
		}
		text = fmt.Sprintf(format, tokensPerSecond)
	}

	if self.isTurnRunning() {
		return style.Peripheral(text)
	}

	return style.Quantity(text)
}
