package segment_test

import (
	"testing"
	"time"

	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/segment/activitySpinner"
	"github.com/BurntSushi/toml"
)

type tomlOptions string

func (self tomlOptions) Read(into any) error {
	_, err := toml.Decode(string(self), into)
	return err
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
