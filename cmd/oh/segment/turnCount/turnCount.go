package turnCount

import (
	"strconv"

	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/style"
)

type state struct {
	getTurnCount func() int
}

func New(getTurnCount func() int) segment.Factory {
	return func(segment.Options) (segment.Segment, error) {
		return state{getTurnCount: getTurnCount}, nil
	}
}

func (self state) Render(segment.Context) string {
	turnCount := self.getTurnCount()
	if turnCount == 0 {
		return ""
	}

	return style.Subtle("#" + strconv.Itoa(turnCount))
}
