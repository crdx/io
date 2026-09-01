package color

import "fmt"

type Colour string

func HEX(value string) Colour {
	return Colour(value)
}

func (self Colour) Sprint(value any) string {
	return fmt.Sprint(value)
}
