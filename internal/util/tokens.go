package util

import "math"

// FormatTokenEstimate estimates tokens from a UTF-8 byte count and renders the result compactly.
func FormatTokenEstimate[Count count](bytes Count, precision int) string {
	const bytesPerToken = 4

	tokens := int64(math.Ceil(float64(bytes) / bytesPerToken))
	return FormatEstimatedTokenCount(tokens, precision)
}

// FormatEstimatedTokenCount renders an estimated token count compactly.
func FormatEstimatedTokenCount[Count count](tokens Count, precision int) string {
	if tokens <= 0 {
		return "~0t"
	}

	const tokensPerKilo = 1000
	if tokens < tokensPerKilo {
		return formatScaledUnit(float64(tokens), precision, "~", "t")
	}

	return formatScaledUnit(float64(tokens)/tokensPerKilo, precision, "~", "Kt")
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
