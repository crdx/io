package contextUsage

import (
	"fmt"
	"strconv"
	"strings"

	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/style"
)

const (
	thousandTokens = 1000
	millionTokens  = 1_000_000

	fullPercentage = 100

	unknown = "?"
)

type state struct {
	usage func() (usedTokens int, totalTokens int)
}

func New(usage func() (usedTokens int, totalTokens int)) segment.Factory {
	return func(options segment.Options) (segment.Segment, error) {
		if err := options.Read(&struct{}{}); err != nil {
			return nil, err
		}

		return state{usage: usage}, nil
	}
}

func (self state) Render(segment.Context) string {
	usedTokens, totalTokens := self.usage()

	return paint(fmt.Sprintf(
		"%s %s/%s",
		formatPercentage(usedTokens, totalTokens),
		formatTokens(usedTokens),
		formatTokens(totalTokens),
	))
}

func formatPercentage(usedTokens int, totalTokens int) string {
	if usedTokens <= 0 || totalTokens <= 0 {
		return unknown + "%"
	}

	used := (usedTokens*fullPercentage + totalTokens/2) / totalTokens

	return strconv.Itoa(min(fullPercentage, used)) + "%"
}

func formatTokens(tokens int) string {
	if tokens <= 0 {
		return unknown
	}

	thousands := round(tokens, thousandTokens)
	if thousands >= thousandTokens {
		return strconv.Itoa(round(tokens, millionTokens)) + "M"
	}

	return strconv.Itoa(max(1, thousands)) + "K"
}

func round(tokens int, size int) int {
	return (tokens + size/2) / size
}

func paint(text string) string {
	var out strings.Builder

	for start := 0; start < len(text); {
		end := start + 1
		for end < len(text) && isCount(text[end]) == isCount(text[start]) {
			end++
		}

		run := text[start:end]
		if isCount(text[start]) {
			out.WriteString(style.Normal(run))
		} else {
			out.WriteString(style.Subtle(run))
		}

		start = end
	}

	return out.String()
}

func isCount(character byte) bool {
	return character >= '0' && character <= '9' || character == unknown[0]
}
