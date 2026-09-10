package util

import (
	"math"
)

const (
	bytesPerToken    = 2.8
	tokenUnit        = "t"
	estimateMark     = "~"
	roughestTokens   = 10
	estimateRounding = 100
)

func EstimateTokenCount[Count count](bytes Count) int64 {
	return int64(math.Ceil(float64(bytes) / bytesPerToken))
}

func FormatTokens[Count count](tokens Count) string {
	return FormatCount(tokens) + tokenUnit
}

func FormatWholeThousands[Count count](tokens Count) string {
	if tokens <= 0 {
		return "0"
	}

	return FormatCount(max(int64(tokens), thousand))
}

func FormatEstimatedTokens[Count count](tokens Count) string {
	if tokens <= 0 {
		return "0" + tokenUnit
	}

	return estimateMark + FormatTokens(roundedEstimate(int64(tokens)))
}

func roundedEstimate(tokens int64) int64 {
	if tokens < roughestTokens || tokens >= thousand {
		return tokens
	}

	return max(int64(math.Round(float64(tokens)/estimateRounding))*estimateRounding, estimateRounding)
}

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
