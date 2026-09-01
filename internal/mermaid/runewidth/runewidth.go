package runewidth

import "crdx.org/io/cmd/oh/width"

func StringWidth(text string) int {
	return width.Of(text)
}

func RuneWidth(character rune) int {
	return width.Of(string(character))
}

func Cells(text string) []string {
	return width.Cells(text)
}
