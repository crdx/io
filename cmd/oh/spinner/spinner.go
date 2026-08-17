// Package spinner defines the animation used while work is underway.
package spinner

import "time"

// Animation is a run of frames something marks time with.
type Animation interface {
	Frame(index int) string

	Rate() time.Duration
}

// Activity rotates a gap through the four dots used by the turn marker.
var Activity Animation = newFrames(300*time.Millisecond, "⠲", "⠴", "⠦", "⠖")

// Braille is the animation shown on a call that is taking its time.
var Braille Animation = newFrames(
	150*time.Millisecond, "⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏",
)

// Frames is an Animation of a fixed run of frames, shown in turn and then again from the start.
type Frames struct {
	frames []string      // the frames in order
	rate   time.Duration // how long each frame is shown for
}

func newFrames(rate time.Duration, frames ...string) Frames {
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
