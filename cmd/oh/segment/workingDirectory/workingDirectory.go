package workingDirectory

import (
	"fmt"
	"path/filepath"

	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/internal/pathutil"
)

const (
	full  = "full"
	base  = "base"
	short = "short"
)

type state struct {
	value string
}

func New(path string) segment.Factory {
	return func(options segment.Options) (segment.Segment, error) {
		var args struct {
			Type string `toml:"type"`
		}

		if err := options.Read(&args); err != nil {
			return nil, err
		}

		value := path

		switch args.Type {
		case "", base:
			value = filepath.Base(path)
		case short:
			value = pathutil.Shorten(path)
		case full:
		default:
			return nil, fmt.Errorf(
				"type is %q, and wants to be omitted or %q, %q, or %q",
				args.Type,
				base,
				short,
				full,
			)
		}

		return state{value: value}, nil
	}
}

func (self state) Render(segment.Context) string {
	return style.Subtle(self.value)
}
