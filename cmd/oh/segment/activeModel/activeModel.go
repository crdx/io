package activeModel

import (
	"slices"
	"strings"

	"crdx.org/io/cmd/oh/model"
	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/segment/fastMode"
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
	isFast       bool
}

func New(name string, effort string, effortLevels []string, isFast bool) segment.Factory {
	return func(segment.Options) (segment.Segment, error) {
		return state{name: name, effort: effort, effortLevels: effortLevels, isFast: isFast}, nil
	}
}

func (self state) Render(segment.Context) string {
	name := model.DisplayName(self.name)
	badge := style.Normal(name[0])
	if len(name) > 1 {
		badge += " " + style.Subtle(name[1])
	}
	if self.isFast {
		badge = fastMode.GetMark(true) + " " + badge
	}

	squares := thinkingSquares(self.effort, self.effortLevels)
	if squares == "" {
		return badge
	}

	return badge + " " + styleThinkingSquares(squares)
}

func thinkingSquares(effort string, effortLevels []string) string {
	ladder := model.EffortOrder[1:]

	if !slices.ContainsFunc(ladder, func(level string) bool { return slices.Contains(effortLevels, level) }) {
		return ""
	}

	var squares strings.Builder

	for _, level := range ladder {
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
	var renderedText strings.Builder
	var subtle strings.Builder

	flushSubtle := func() {
		if subtle.Len() > 0 {
			renderedText.WriteString(style.Subtle(subtle.String()))
			subtle.Reset()
		}
	}

	for _, square := range squares {
		if string(square) == filledSquare {
			flushSubtle()
			renderedText.WriteString(style.ChosenRow(string(square)))
		} else {
			subtle.WriteRune(square)
		}
	}
	flushSubtle()

	return renderedText.String()
}
