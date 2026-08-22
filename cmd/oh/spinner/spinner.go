// Package spinner defines the animation used while work is underway.
package spinner

import "time"

// Animation is a run of frames something marks time with.
type Animation interface {
	Frame(index int) string

	Rate() time.Duration
}

// Activity sends a twinkling star back and forth while work is underway.
var Activity Animation = Of(125*time.Millisecond, "✦·", "·✦", "·✧", "✧·")

// Frames is an Animation of a fixed run of frames, shown in turn and then again from the start.
type Frames struct {
	frames []string
	rate   time.Duration // how long each frame is shown for
}

func Of(rate time.Duration, frames ...string) Frames {
	return Frames{frames: frames, rate: rate}
}

// Frame is the frame at an index, counted round the run.
func (self Frames) Frame(index int) string {
	return self.frames[index%len(self.frames)]
}

// Rate is how long a frame is shown for.
func (self Frames) Rate() time.Duration {
	return self.rate
}
