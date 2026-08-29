package config

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

const (
	kilobyte = 1 << 10
	megabyte = 1 << 20
	gigabyte = 1 << 30
)

type Size struct {
	Bytes int
}

func (self *Size) UnmarshalText(text []byte) error {
	written := strings.TrimSpace(string(text))

	multiplier := 1

	switch {
	case strings.HasSuffix(strings.ToUpper(written), "K"):
		multiplier = kilobyte
	case strings.HasSuffix(strings.ToUpper(written), "M"):
		multiplier = megabyte
	case strings.HasSuffix(strings.ToUpper(written), "G"):
		multiplier = gigabyte
	}

	if multiplier > 1 {
		written = written[:len(written)-1]
	}

	count, err := strconv.Atoi(strings.TrimSpace(written))
	if err != nil || count < 0 {
		return fmt.Errorf("%q is not a size; write a count of bytes, or one with a K, M or G after it", text)
	}

	if count > math.MaxInt/multiplier {
		return fmt.Errorf("%q is larger than this machine can count", text)
	}

	self.Bytes = count * multiplier

	return nil
}
