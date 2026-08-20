package spinner_test

import (
	"testing"
	"time"

	"crdx.org/io/cmd/oh/spinner"
	"crdx.org/io/cmd/oh/style"
)

func TestActivityIsTwoCellsWide(t *testing.T) {
	for frame := range 4 {
		value := spinner.Activity.Frame(frame)
		if width := style.Width(value); width != 2 {
			t.Errorf("frame %d is %d cells wide: %q", frame, width, value)
		}
	}
}

func TestActivityMovesAtATwinklingPace(t *testing.T) {
	if rate := spinner.Activity.Rate(); rate != 125*time.Millisecond {
		t.Errorf("got rate %s, want 125ms", rate)
	}
}
