package activeModel

import (
	"slices"
	"strings"

	"crdx.org/io/cmd/oh/model"
	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/style"
)

const (
	filledSquare      = "▪"
	emptySquare       = "▫"
	unsupportedSquare = "·"
)

type state struct {
	name         string
	effort       string
	effortLevels []string
}

func New(name string, effort string, effortLevels []string) segment.Factory {
	return func(segment.Options) (segment.Segment, error) {
		return state{name: name, effort: effort, effortLevels: effortLevels}, nil
	}
}

func (self state) Render(segment.Context) string {
	name := model.DisplayName(self.name)
	badge := style.Normal(name[0])
	if len(name) > 1 {
		badge += " " + style.Subtle(name[1])
	}

	squares := thinkingSquares(self.effort, self.effortLevels)
	if squares == "" {
		return badge
	}

	return badge + " " + styleThinkingSquares(squares)
}

func thinkingSquares(effort string, effortLevels []string) string {
	if len(effortLevels) == 0 {
		return ""
	}

	var squares strings.Builder

	for _, level := range model.EffortOrder[1:] {
		switch {
		case !slices.Contains(effortLevels, level):
			squares.WriteString(unsupportedSquare)
		case level == effort:
			squares.WriteString(filledSquare)
		default:
			squares.WriteString(emptySquare)
		}
	}

	return squares.String()
}

func styleThinkingSquares(squares string) string {
	var rendered strings.Builder
	var subtle strings.Builder

	flushSubtle := func() {
		if subtle.Len() > 0 {
			rendered.WriteString(style.Subtle(subtle.String()))
			subtle.Reset()
		}
	}

	for _, square := range squares {
		if string(square) == filledSquare {
			flushSubtle()
			rendered.WriteString(style.Chosen(string(square)))
		} else {
			subtle.WriteRune(square)
		}
	}
	flushSubtle()

	return rendered.String()
}
