package segment

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

type Position int

const (
	TopLeft Position = iota
	TopCenter
	TopRight
	BottomLeft
	BottomCenter
	BottomRight
)

var Positions = []Position{TopLeft, TopCenter, TopRight, BottomLeft, BottomCenter, BottomRight}

var positionNames = map[Position]string{
	TopLeft:      "top.left",
	TopCenter:    "top.center",
	TopRight:     "top.right",
	BottomLeft:   "bottom.left",
	BottomCenter: "bottom.center",
	BottomRight:  "bottom.right",
}

func (self Position) String() string {
	return positionNames[self]
}

type Context struct {
	Above int
	Below int
}

type Instance interface {
	Render(Context) string
}

type (
	Unmarshall func(any) error
	Factory    func(Unmarshall) (Instance, error)
	Set        map[string]Factory
	Layout     map[Position][]Instance
)

func (self Set) Build(name string, position Position, options Unmarshall) (Instance, error) {
	factory, ok := self[name]
	if !ok {
		return nil, fmt.Errorf(
			"%s: there is no segment called %q, only: %s",
			position, name, strings.Join(self.Names(), ", "),
		)
	}

	built, err := factory(options)
	if err != nil {
		return nil, fmt.Errorf("%s: %s: %w", position, name, err)
	}

	return built, nil
}

func (self Set) Names() []string {
	names := make([]string, 0, len(self))
	for name := range self {
		names = append(names, name)
	}

	slices.Sort(names)

	return names
}

type Ticker interface {
	Rate() time.Duration
}

func (self Layout) Rate() time.Duration {
	var fastest time.Duration

	for _, position := range Positions {
		for _, placed := range self[position] {
			ticker, ok := placed.(Ticker)
			if !ok {
				continue
			}

			if rate := ticker.Rate(); rate > 0 && (fastest == 0 || rate < fastest) {
				fastest = rate
			}
		}
	}

	return fastest
}
