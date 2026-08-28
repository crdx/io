package width

import (
	"iter"
	"strings"

	"crdx.org/io/cmd/oh/escape"
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
	runes := []rune(text)

	for i := 0; i < len(runes); {
		if runes[i] == '\x1b' {
			sequence := escape.GetSequence(runes, i)
			cells += sequence.Cells
			i = sequence.End
			continue
		}

		end := i + 1
		for end < len(runes) && runes[end] != '\x1b' {
			end++
		}
		for _, graphemeCells := range Graphemes(string(runes[i:end])) {
			cells += graphemeCells
		}
		i = end
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
