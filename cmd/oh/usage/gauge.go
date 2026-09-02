package usage

import (
	"image"
	"image/color"

	"crdx.org/io/cmd/oh/graphics"
	"crdx.org/io/cmd/oh/style"
)

const (
	barPadding   = 5
	tickCells    = 8
	tickDivisor  = 2
	trackDivisor = 3
)

type Graphics struct {
	CellWidth  int
	CellHeight int
}

func gaugePlacement(limit Limit, pace Pace, cells int, drawing Graphics) (string, bool) {
	return graphics.Place(gaugePicture(limit, pace, cells, drawing), cells)
}

func gaugePicture(limit Limit, pace Pace, cells int, drawing Graphics) *image.RGBA {
	pixelWidth := cells * drawing.CellWidth
	pixelHeight := drawing.CellHeight

	picture := image.NewRGBA(image.Rect(0, 0, pixelWidth, pixelHeight))

	padding := pixelHeight / barPadding
	fillWidth := limit.UsedPercent * pixelWidth / percentCeiling

	tickColumn := -1
	tickHalf := max(1, drawing.CellWidth/tickCells)

	if limit.ExpectedPercent != nil {
		tickColumn = *limit.ExpectedPercent * pixelWidth / percentCeiling
	}

	fill := paceColour(pace)
	track := scale(style.DimColour, trackDivisor)

	for y := padding; y < pixelHeight-padding; y++ {
		for x := range pixelWidth {
			isFilled := x < fillWidth

			pixel := track
			if isFilled {
				pixel = fill
			}

			if tickColumn >= 0 && absolute(x-tickColumn) <= tickHalf {
				pixel = style.DimColour
				if isFilled {
					pixel = scale(fill, tickDivisor)
				}
			}

			picture.SetRGBA(x, y, pixel)
		}
	}

	return picture
}

func paceColour(pace Pace) color.RGBA {
	switch pace {
	case PaceAhead:
		return style.ChangeColour
	case PaceCritical:
		return style.FailureColour
	case PaceEven:
	}

	return style.InformationColour
}

func scale(value color.RGBA, divisor uint8) color.RGBA {
	return color.RGBA{
		R: value.R / divisor,
		G: value.G / divisor,
		B: value.B / divisor,
		A: value.A,
	}
}

func absolute(value int) int {
	if value < 0 {
		return -value
	}

	return value
}
