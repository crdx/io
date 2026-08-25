package util

import (
	"math"
)

const (
	thousandTokens = 1000
	millionTokens  = 1_000_000
)

func formatLargeTokenCount(tokens float64, precision int, prefix string) string {
	if tokens >= millionTokens {
		return formatScaledUnit(tokens/millionTokens, precision, prefix, "Mt")
	}

	return formatScaledUnit(tokens/thousandTokens, precision, prefix, "Kt")
}

// EstimateTokenCount estimates how many tokens a UTF-8 byte count is worth.
func EstimateTokenCount[Count count](bytes Count) int64 {
	const bytesPerToken = 4

	return int64(math.Ceil(float64(bytes) / bytesPerToken))
}

// FormatTokenEstimate estimates tokens from a UTF-8 byte count and renders the result compactly.
func FormatTokenEstimate[Count count](bytes Count, precision int) string {
	return FormatEstimatedTokenCount(EstimateTokenCount(bytes), precision)
}

// FormatEstimatedTokenCount renders an estimate compactly. A handful of tokens is said exactly,
// since rounding one to a hundred says more than it knows. Anything larger is rounded to the
// nearest hundred, but never down to nothing: only nothing reads as nothing.
func FormatEstimatedTokenCount[Count count](tokens Count, precision int) string {
	if tokens <= 0 {
		return "0t"
	}

	n := float64(tokens)
	if n < 10 {
		return formatScaledUnit(n, precision, "~", "t")
	}

	if n < thousandTokens {
		n = max(math.Round(n/100)*100, 100)
	}

	if n < thousandTokens {
		return formatScaledUnit(n, precision, "~", "t")
	}

	return formatLargeTokenCount(n, precision, "~")
}

// EstimateImageTokenCount estimates the 32-pixel patches used to encode an image.
func EstimateImageTokenCount(width int, height int) int64 {
	if width <= 0 || height <= 0 {
		return 0
	}

	const (
		patchSize      = 32
		maximumPatches = 1536
	)

	horizontalPatches := (width + patchSize - 1) / patchSize
	verticalPatches := (height + patchSize - 1) / patchSize
	return int64(min(horizontalPatches*verticalPatches, maximumPatches))
}
