package util

import (
	"math"
)

const (
	thousandTokens = 1000
	millionTokens  = 1_000_000
	bytesPerToken  = 2.8
	tokenPrecision = 2
	tokenUnit      = "t"
	estimateMark   = "~"
	noTokenUnit    = ""
	noEstimate     = ""
)

func formatLargeTokenCount(tokens float64, prefix string, unit string) string {
	if tokens >= millionTokens {
		return formatScaledUnit(tokens/millionTokens, tokenPrecision, prefix, "M"+unit)
	}

	return formatScaledUnit(tokens/thousandTokens, tokenPrecision, prefix, "K"+unit)
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
		return formatScaledUnit(estimate, tokenPrecision, estimateMark, tokenUnit)
	}

	if estimate < thousandTokens {
		estimate = max(math.Round(estimate/100)*100, 100)
	}

	if estimate < thousandTokens {
		return formatScaledUnit(estimate, tokenPrecision, estimateMark, tokenUnit)
	}

	return formatLargeTokenCount(estimate, estimateMark, tokenUnit)
}

func FormatTokenCount[Count count](tokens Count) string {
	if tokens <= 0 {
		return "0K"
	}

	return formatLargeTokenCount(max(float64(tokens), thousandTokens), noEstimate, noTokenUnit)
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
