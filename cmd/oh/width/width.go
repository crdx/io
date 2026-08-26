package width

import (
	"iter"
	"strings"

	"github.com/rivo/uniseg"
)

func Graphemes(text string) iter.Seq2[string, int] {
	return func(yield func(string, int) bool) {
		graphemes := uniseg.NewGraphemes(text)
		for graphemes.Next() {
			grapheme := graphemes.Str()
			if !yield(grapheme, graphemeWidth(grapheme, graphemes.Width())) {
				return
			}
		}
	}
}

func Of(text string) int {
	cells := 0
	for _, graphemeCells := range Graphemes(text) {
		cells += graphemeCells
	}
	return cells
}

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
