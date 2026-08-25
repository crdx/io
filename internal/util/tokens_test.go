package util_test

import (
	"testing"

	"crdx.org/io/internal/util"
)

func TestFormatTokenEstimateUsesFourBytesPerToken(t *testing.T) {
	for bytes, want := range map[int64]string{
		-1:   "0t",
		0:    "0t",
		1:    "~1t",
		4:    "~1t",
		5:    "~2t",
		740:  "~200t",
		4000: "~1Kt",
		5000: "~1.2Kt",
	} {
		if got := util.FormatTokenEstimate(bytes); got != want {
			t.Errorf("FormatTokenEstimate(%d) = %q, want %q", bytes, got, want)
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
		1_234_567: "~1.2Mt",
	} {
		if got := util.FormatEstimatedTokenCount(tokens); got != want {
			t.Errorf("FormatEstimatedTokenCount(%d) = %q, want %q", tokens, got, want)
		}
	}
}

func TestFormatTokenEstimateIsWrittenToTwoSignificantDigits(t *testing.T) {
	if got := util.FormatTokenEstimate(4996); got != "~1.2Kt" {
		t.Errorf("got %q, want ~1.2Kt", got)
	}
	if got := util.FormatTokenEstimate(400_000); got != "~100Kt" {
		t.Errorf("got %q, want ~100Kt without scientific notation", got)
	}
}

func TestFormatTokenCountDropsTheUnitAndSaysNothingUsedAsNothing(t *testing.T) {
	for tokens, want := range map[int64]string{
		-1:        "0K",
		0:         "0K",
		400:       "1K",
		5000:      "5K",
		64_000:    "64K",
		92_501:    "93K",
		274_000:   "274K",
		1_048_576: "1M",
		1_600_000: "1.6M",
	} {
		if got := util.FormatTokenCount(tokens); got != want {
			t.Errorf("FormatTokenCount(%d) = %q, want %q", tokens, got, want)
		}
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
