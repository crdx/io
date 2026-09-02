package util

import (
	"math"
)

const (
	bytesPerToken = 2.8
	tokenUnit     = "t"
	estimateMark  = "~"
	noTokenUnit   = ""
	noEstimate    = ""
)

func formatLargeTokenCount(tokens float64, prefix string, unit string) string {
	if tokens >= million {
		return formatScaledUnit(tokens/million, countPrecision, prefix, "M"+unit)
	}

	return formatScaledUnit(tokens/thousand, countPrecision, prefix, "K"+unit)
}

func EstimateTokenCount[Count count](bytes Count) int64 {
	return int64(math.Ceil(float64(bytes) / bytesPerToken))
}

func FormatTokenEstimate[Count count](bytes Count) string {
	return FormatEstimatedTokenCount(EstimateTokenCount(bytes))
}

func FormatEstimatedTokenCount[Count count](tokens Count) string {
	if tokens <= 0 {
		return "0" + tokenUnit
	}

	estimate := float64(tokens)
	if estimate < 10 {
		return formatScaledUnit(estimate, countPrecision, estimateMark, tokenUnit)
	}

	if estimate < thousand {
		estimate = max(math.Round(estimate/100)*100, 100)
	}

	if estimate < thousand {
		return formatScaledUnit(estimate, countPrecision, estimateMark, tokenUnit)
	}

	return formatLargeTokenCount(estimate, estimateMark, tokenUnit)
}

func FormatTokenCount[Count count](tokens Count) string {
	if tokens <= 0 {
		return "0K"
	}

	return formatLargeTokenCount(max(float64(tokens), thousand), noEstimate, noTokenUnit)
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
