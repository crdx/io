package util_test

import (
	"testing"

	"crdx.org/io/internal/util"
)

func TestFormatTokenCountRendersMeasuredCountsWithoutAnEstimateMarker(t *testing.T) {
	for tokens, want := range map[int64]string{
		0:         "0t",
		999:       "999t",
		1000:      "1Kt",
		12_345:    "12.3Kt",
		200_000:   "200Kt",
		1_000_000: "1Mt",
		1_234_567: "1.23Mt",
	} {
		if got := util.FormatTokenCount(tokens, 3); got != want {
			t.Errorf("FormatTokenCount(%d, 3) = %q, want %q", tokens, got, want)
		}
	}
}

func TestFormatTokenEstimateUsesFourBytesPerToken(t *testing.T) {
	for bytes, want := range map[int64]string{
		-1:   "0t",
		0:    "0t",
		1:    "~1t",
		4:    "~1t",
		5:    "~2t",
		740:  "~200t",
		4000: "~1Kt",
		5000: "~1.25Kt",
	} {
		if got := util.FormatTokenEstimate(bytes, 3); got != want {
			t.Errorf("FormatTokenEstimate(%d, 3) = %q, want %q", bytes, got, want)
		}
	}
}

func TestFormatEstimatedTokenCountRoundsSubKiloValuesToNearestHundred(t *testing.T) {
	for tokens, want := range map[int64]string{
		0:         "0t",
		1:         "~1t",
		9:         "~9t",
		10:        "~100t",
		49:        "~100t",
		50:        "~100t",
		949:       "~900t",
		950:       "~1Kt",
		999:       "~1Kt",
		1000:      "~1Kt",
		1_000_000: "~1Mt",
		1_234_567: "~1.23Mt",
	} {
		if got := util.FormatEstimatedTokenCount(tokens, 3); got != want {
			t.Errorf("FormatEstimatedTokenCount(%d, 3) = %q, want %q", tokens, got, want)
		}
	}
}

func TestFormatTokenEstimateHonoursPrecision(t *testing.T) {
	if got := util.FormatTokenEstimate(4996, 2); got != "~1.2Kt" {
		t.Errorf("got %q, want ~1.2Kt", got)
	}
	if got := util.FormatTokenEstimate(400_000, 2); got != "~100Kt" {
		t.Errorf("got %q, want ~100Kt without scientific notation", got)
	}
}

func TestImageTokensAreEstimatedFromPatches(t *testing.T) {
	for name, test := range map[string]struct {
		width  int
		height int
		want   int64
	}{
		"invalid":       {0, 100, 0},
		"one patch":     {32, 32, 1},
		"partial patch": {33, 32, 2},
		"capped":        {2098, 1136, 1536},
	} {
		t.Run(name, func(t *testing.T) {
			if got := util.EstimateImageTokenCount(test.width, test.height); got != test.want {
				t.Errorf("got %d, want %d", got, test.want)
			}
		})
	}
}
