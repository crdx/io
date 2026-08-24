// Package color removes upstream class colours so oh can apply its own terminal palette.
package color

import "fmt"

// Colour is an ignored Mermaid class colour.
type Colour string

// HEX returns an ignored Mermaid class colour.
func HEX(value string) Colour {
	return Colour(value)
}

// Sprint returns text without applying an ANSI colour.
func (self Colour) Sprint(value any) string {
	return fmt.Sprint(value)
}
