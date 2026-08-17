// Package width approximates terminal cell widths. Its table covers common East Asian, Hangul, and
// emoji ranges and must be updated as Unicode grows.
package width

import (
	"slices"
	"unicode"
)

// Of is how many cells text takes up.
func Of(text string) int {
	cells := 0

	for _, value := range text {
		cells += Rune(value)
	}

	return cells
}

// Rune is how many cells one character takes up.
func Rune(value rune) int {
	switch {
	case unicode.In(value, unicode.Mn, unicode.Me, unicode.Cf, unicode.Cc):
		return 0
	case wide(value):
		return 2
	}

	return 1
}

// Cut returns the longest prefix that fits and its cell width.
func Cut(text string, cells int) (string, int) {
	takenCells := 0

	for index, value := range text {
		size := Rune(value)
		if takenCells+size > cells {
			return text[:index], takenCells
		}

		takenCells += size
	}

	return text, takenCells
}

func wide(value rune) bool {
	if value < spans[0].first {
		return false
	}

	index, found := slices.BinarySearchFunc(spans, value, func(one span, want rune) int {
		switch {
		case want < one.first:
			return 1
		case want > one.last:
			return -1
		}

		return 0
	})

	return found && index < len(spans)
}
