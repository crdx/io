package util

import (
	"fmt"
	"math"
	"strings"
)

type count interface {
	~int | ~int64 | ~uint64
}

func FormatBytes[Count count](bytes Count, precision int) string {
	if bytes <= 0 {
		return "0B"
	}

	const (
		kilobyte = 1 << 10
		megabyte = 1 << 20
		gigabyte = 1 << 30
		terabyte = 1 << 40
	)

	switch {
	case bytes >= terabyte:
		return formatScaledUnit(float64(bytes)/terabyte, precision, "", "T")
	case bytes >= gigabyte:
		return formatScaledUnit(float64(bytes)/gigabyte, precision, "", "G")
	case bytes >= megabyte:
		return formatScaledUnit(float64(bytes)/megabyte, precision, "", "M")
	case bytes >= kilobyte:
		return formatScaledUnit(float64(bytes)/kilobyte, precision, "", "K")
	default:
		return fmt.Sprintf("%dB", bytes)
	}
}

func formatScaledUnit(value float64, precision int, prefix string, unit string) string {
	wholeDigits := int(math.Floor(math.Log10(value))) + 1
	decimalPlaces := max(precision-wholeDigits, 0)
	formattedNumber := fmt.Sprintf("%.*f", decimalPlaces, value)
	if decimalPlaces > 0 {
		formattedNumber = strings.TrimRight(strings.TrimRight(formattedNumber, "0"), ".")
	}
	return prefix + formattedNumber + unit
}
