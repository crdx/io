package activity

import (
	"errors"
	"fmt"
	"time"

	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/spinner"
	"crdx.org/io/cmd/oh/style"
)

var _ segment.Ticker = state{}

type state struct {
	turn      func() (isRunning bool, frameIndex int)
	animation spinner.Animation
	idle      string
}

func New(turn func() (isRunning bool, frameIndex int)) segment.Factory {
	return func(options segment.Options) (segment.Segment, error) {
		var args struct {
			Idle   string        `toml:"idle"`
			Frames []string      `toml:"frames"`
			Rate   time.Duration `toml:"rate"`
		}

		if err := options.Read(&args); err != nil {
			return nil, err
		}

		if len(args.Frames) == 0 {
			return nil, errors.New("frames are empty, so there is nothing to turn through")
		}

		if args.Rate <= 0 {
			return nil, fmt.Errorf("rate is %s, and wants to be longer than nothing", args.Rate)
		}

		width := style.Width(args.Idle)
		for _, frame := range args.Frames {
			if style.Width(frame) != width {
				return nil, fmt.Errorf(
					"%q is %d cells wide and %q is %d, so the rule beside them would shift",
					frame, style.Width(frame), args.Idle, width,
				)
			}
		}

		return state{
			turn:      turn,
			animation: spinner.Of(args.Rate, args.Frames...),
			idle:      args.Idle,
		}, nil
	}
}

func (self state) RefreshInterval() time.Duration {
	return self.animation.RefreshInterval()
}

func (self state) Render(segment.Context) string {
	isRunning, frameIndex := self.turn()
	if !isRunning {
		return style.Withheld(self.idle)
	}

	return style.Spinner(self.animation.Frame(frameIndex))
}
