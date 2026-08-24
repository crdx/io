package contextUsage

import (
	"fmt"

	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/internal/util"
)

const oneMillionTokenContextWindow = 1_000_000

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
	used := "?"
	total := "?"
	percentage := "?%"

	if usedTokens > 0 {
		used = util.FormatTokenCount(usedTokens, 3)
	}
	if totalTokens == oneMillionTokenContextWindow {
		total = "1Mt"
	} else if totalTokens > 0 {
		total = util.FormatTokenCount(totalTokens, 3)
	}
	if usedTokens > 0 && totalTokens > 0 {
		percentage = fmt.Sprintf("%d%%", min(100, (usedTokens*100+totalTokens/2)/totalTokens))
	}

	return style.Subtle(fmt.Sprintf("%s %s/%s", percentage, used, total))
}
