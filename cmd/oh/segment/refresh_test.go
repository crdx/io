package segment_test

import (
	"testing"
	"time"

	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/segment/activitySpinner"
	"github.com/BurntSushi/toml"
)

type tomlOptions string

type dynamicIdleSegment struct {
	idleInterval time.Duration
}

func (self *dynamicIdleSegment) Render(segment.Context) string  { return "" }
func (self *dynamicIdleSegment) RefreshInterval() time.Duration { return 125 * time.Millisecond }
func (self *dynamicIdleSegment) Persistent() bool               { return true }
func (self *dynamicIdleSegment) IdleRefreshInterval() time.Duration {
	return self.idleInterval
}

func (self tomlOptions) Read(into any) error {
	_, err := toml.Decode(string(self), into)
	return err
}

func TestAChangingIdleIntervalIsReadEachTime(t *testing.T) {
	built := &dynamicIdleSegment{idleInterval: time.Second}
	layout := segment.Layout{segment.BottomLeft: {built}}

	if got := layout.IdleRefreshInterval(); got != time.Second {
		t.Errorf("initial idle interval = %s", got)
	}

	built.idleInterval = 125 * time.Millisecond

	if got := layout.IdleRefreshInterval(); got != 125*time.Millisecond {
		t.Errorf("changed idle interval = %s", got)
	}
}

func TestTheActivitySegmentDrivesTheRedrawTicker(t *testing.T) {
	options := tomlOptions("idle = \"·\"\nframes = [\"*\"]\nrate = \"125ms\"\n")

	built, err := activitySpinner.New(func() (bool, int) { return true, 0 })(options)
	if err != nil {
		t.Fatal(err)
	}

	layout := segment.Layout{segment.BottomLeft: {built}}
	if got := layout.RefreshInterval(); got != 125*time.Millisecond {
		t.Errorf("expected the bar to ask for a redraw every 125ms, got %s", got)
	}
}
