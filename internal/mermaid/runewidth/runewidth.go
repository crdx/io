// Package runewidth adapts oh's terminal cell measurement to the vendored renderer.
package runewidth

import "crdx.org/io/cmd/oh/width"

// StringWidth returns the number of terminal cells occupied by text.
func StringWidth(text string) int {
	return width.Of(text)
}

// RuneWidth returns the number of terminal cells occupied by a rune.
func RuneWidth(character rune) int {
	return width.Of(string(character))
}
