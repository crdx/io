package width

import (
	"slices"
	"strings"
	"unicode"

	"github.com/rivo/uniseg"
)

// Of is how many cells text takes up.
func Of(text string) int {
	cells := 0
	graphemes := uniseg.NewGraphemes(text)
	for graphemes.Next() {
		cells += graphemeWidth(graphemes.Str(), graphemes.Width())
	}
	return cells
}

// Rune is how many cells one character takes up.
func Rune(value rune) int {
	switch {
	case unicode.In(value, unicode.Mn, unicode.Me, unicode.Cf, unicode.Cc):
		return 0
	case isWide(value):
		return 2
	}

	return 1
}

// Cut returns the longest prefix that fits and its cell width.
func Cut(text string, cells int) (string, int) {
	takenCells := 0
	graphemes := uniseg.NewGraphemes(text)
	for graphemes.Next() {
		size := graphemeWidth(graphemes.Str(), graphemes.Width())
		if takenCells+size > cells {
			start, _ := graphemes.Positions()
			return text[:start], takenCells
		}
		takenCells += size
	}
	return text, takenCells
}

// Cells expands text into terminal cells without splitting grapheme clusters.
func Cells(text string) []string {
	var cells []string
	graphemes := uniseg.NewGraphemes(text)
	for graphemes.Next() {
		graphemeCells := graphemeWidth(graphemes.Str(), graphemes.Width())
		if graphemeCells == 0 {
			continue
		}
		cells = append(cells, graphemes.Str())
		for range graphemeCells - 1 {
			cells = append(cells, "")
		}
	}
	return cells
}

func graphemeWidth(grapheme string, measuredWidth int) int {
	if strings.ContainsRune(grapheme, '\u20e3') {
		return 2
	}
	return measuredWidth
}

func isWide(value rune) bool {
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
