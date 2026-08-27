package localTime

import (
	"time"

	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/style"
)

var _ segment.Refresher = state{}

const defaultFormat = "15:04"

var reference = time.Date(2006, time.January, 2, 15, 4, 5, 0, time.UTC)

type state struct {
	now         func() time.Time
	format      string
	granularity time.Duration
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

		return state{
			now:         now,
			format:      args.Format,
			granularity: granularityOf(args.Format),
		}, nil
	}
}

// NextRefresh is the moment the clock face changes, which is the next whole minute for a format
// telling the time to the minute, and the next whole second for one telling it to the second. A
// clock showing 15:04 has no business redrawing the bar sixty times over while it says the same
// thing.
func (self state) NextRefresh(phase segment.Phase) time.Time {
	return phase.At.Truncate(self.granularity).Add(self.granularity)
}

func (self state) Render(segment.Context) string {
	return style.Subtle(self.now().Format(self.format))
}

func granularityOf(format string) time.Duration {
	if reference.Format(format) != reference.Add(time.Second).Format(format) {
		return time.Second
	}

	return time.Minute
}
