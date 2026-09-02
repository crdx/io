package util

import (
	"strconv"
)

const (
	thousand       = 1000
	million        = 1_000_000
	countPrecision = 2
)

func FormatCount[Count count](value Count) string {
	switch {
	case value <= 0:
		return "0"
	case value >= million:
		return formatScaledUnit(float64(value)/million, countPrecision, "", "M")
	case value >= thousand:
		return formatScaledUnit(float64(value)/thousand, countPrecision, "", "K")
	default:
		return strconv.FormatInt(int64(value), 10)
	}
}
