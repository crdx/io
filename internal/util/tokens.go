package util

import (
	"math"
)

const (
	thousandTokens = 1000
	millionTokens  = 1_000_000

	tokenPrecision = 2

	tokenUnit    = "t"
	estimateMark = "~"
	noTokenUnit  = ""
	noEstimate   = ""
)

func formatLargeTokenCount(tokens float64, prefix string, unit string) string {
	if tokens >= millionTokens {
		return formatScaledUnit(tokens/millionTokens, tokenPrecision, prefix, "M"+unit)
	}

	return formatScaledUnit(tokens/thousandTokens, tokenPrecision, prefix, "K"+unit)
}

// EstimateTokenCount estimates how many tokens a UTF-8 byte count is worth.
func EstimateTokenCount[Count count](bytes Count) int64 {
	const bytesPerToken = 4

	return int64(math.Ceil(float64(bytes) / bytesPerToken))
}

// FormatTokenEstimate estimates tokens from a UTF-8 byte count and renders the result compactly.
func FormatTokenEstimate[Count count](bytes Count) string {
	return FormatEstimatedTokenCount(EstimateTokenCount(bytes))
}

// FormatEstimatedTokenCount renders an estimate compactly, marked with a tilde and carrying the
// token unit. A handful of tokens is said exactly, since rounding one to a hundred says more than
// it knows. Anything larger is rounded to the nearest hundred, but never down to nothing: only
// nothing reads as nothing.
func FormatEstimatedTokenCount[Count count](tokens Count) string {
	if tokens <= 0 {
		return "0" + tokenUnit
	}

	n := float64(tokens)
	if n < 10 {
		return formatScaledUnit(n, tokenPrecision, estimateMark, tokenUnit)
	}

	if n < thousandTokens {
		n = max(math.Round(n/100)*100, 100)
	}

	if n < thousandTokens {
		return formatScaledUnit(n, tokenPrecision, estimateMark, tokenUnit)
	}

	return formatLargeTokenCount(n, estimateMark, tokenUnit)
}

// FormatTokenCount renders a counted, rather than estimated, number of tokens somewhere with no
// room to spare, so it drops the token unit and leaves the reader to know what is being counted. A
// count too small to fill a thousand is still said as a thousand, since something used is not
// nothing.
func FormatTokenCount[Count count](tokens Count) string {
	if tokens <= 0 {
		return "0K"
	}

	return formatLargeTokenCount(max(float64(tokens), thousandTokens), noEstimate, noTokenUnit)
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
