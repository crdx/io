package localTime

import (
	"time"

	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/style"
)

var _ segment.Persister = state{}

const defaultFormat = "15:04"

type state struct {
	now    func() time.Time
	format string
}

func New(now func() time.Time) segment.Factory {
	return func(options segment.Options) (segment.Segment, error) {
		var args struct {
			Format string `toml:"format"`
		}

		if err := options.Read(&args); err != nil {
			return nil, err
		}

		if args.Format == "" {
			args.Format = defaultFormat
		}

		return state{now: now, format: args.Format}, nil
	}
}

func (self state) RefreshInterval() time.Duration {
	return time.Second
}

func (self state) Persistent() bool {
	return true
}

func (self state) Render(segment.Context) string {
	return style.Subtle(self.now().Format(self.format))
}
