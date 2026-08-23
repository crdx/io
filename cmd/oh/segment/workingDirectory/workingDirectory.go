package workingDirectory

import (
	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/internal/pathutil"
)

type state struct {
	value string
}

func New(path string) segment.Factory {
	return func(segment.Options) (segment.Segment, error) {
		return state{value: pathutil.Abbr(path)}, nil
	}
}

func (self state) Render(segment.Context) string {
	return style.Subtle(self.value)
}
