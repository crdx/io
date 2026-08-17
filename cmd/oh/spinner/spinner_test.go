package spinner_test

import (
	"testing"

	"crdx.org/io/cmd/oh/spinner"
)

func TestActivityUsesOnlyTheBottomFourDots(t *testing.T) {
	const bottomFour = 0x36

	for frame := range 4 {
		values := []rune(spinner.Activity.Frame(frame))
		if len(values) != 1 {
			t.Fatalf("frame %d is not one rune: %q", frame, string(values))
		}

		dots := values[0] - 0x2800
		if dots & ^rune(bottomFour) != 0 {
			t.Errorf("frame %d uses dots outside the bottom four: %q", frame, string(values))
		}
	}
}
