package turnCount

import (
	"strconv"

	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/style"
)

type state struct {
	taken func() int
}

func New(taken func() int) segment.Factory {
	return func(segment.Options) (segment.Segment, error) {
		return state{taken: taken}, nil
	}
}

func (self state) Render(segment.Context) string {
	taken := self.taken()
	if taken == 0 {
		return ""
	}

	return style.Subtle("#" + strconv.Itoa(taken))
}
