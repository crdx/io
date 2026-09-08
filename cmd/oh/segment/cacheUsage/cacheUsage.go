package cacheUsage

import (
	"strconv"
	"strings"
	"time"

	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/internal/util"
)

const (
	fullPercentage = 100
)

type state struct {
	usage    func() (readTokens int, askedTokens int)
	lifetime func() time.Duration
}

func New(
	usage func() (readTokens int, askedTokens int),
	lifetime func() time.Duration,
) segment.Factory {
	return func(options segment.Options) (segment.Segment, error) {
		if err := options.Read(&struct{}{}); err != nil {
			return nil, err
		}

		return state{usage: usage, lifetime: lifetime}, nil
	}
}

func (self state) Render(segment.Context) string {
	var parts []string

	if lifetime := self.lifetime(); lifetime > 0 {
		parts = append(parts, style.Information(util.CompactDuration(lifetime)))
	}

	readTokens, askedTokens := self.usage()
	if share, isKnown := readShare(readTokens, askedTokens); isKnown {
		parts = append(parts, style.Quantity(strconv.Itoa(share)+"%"))
	}

	return strings.Join(parts, " ")
}

func readShare(readTokens int, askedTokens int) (int, bool) {
	if askedTokens <= 0 {
		return 0, false
	}

	return min(fullPercentage, (readTokens*fullPercentage+askedTokens/2)/askedTokens), true
}
