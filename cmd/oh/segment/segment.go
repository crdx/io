package segment

import (
	"fmt"
	"maps"
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

var Positions = []Position{
	TopLeft,
	TopCenter,
	TopRight,
	BottomLeft,
	BottomCenter,
	BottomRight,
}

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
	HiddenLinesAbove int
	HiddenLinesBelow int
}

type Segment interface {
	Render(Context) string
}

type Options interface {
	Read(into any) error
}

type (
	Factory  func(Options) (Segment, error)
	Registry map[string]Factory
	Layout   map[Position][]Segment
)

func (self Registry) Build(name string, position Position, options Options) (Segment, error) {
	buildSegment, ok := self[name]
	if !ok {
		return nil, fmt.Errorf(
			"%s: there is no segment called %q, only: %s",
			position,
			name,
			strings.Join(self.Available(), ", "),
		)
	}

	segment, err := buildSegment(options)
	if err != nil {
		return nil, fmt.Errorf("%s: %s: %w", position, name, err)
	}

	return segment, nil
}

func (self Registry) Available() []string {
	return slices.Sorted(maps.Keys(self))
}

type Ticker interface {
	RefreshInterval() time.Duration
}

type Persister interface {
	Ticker
	Persistent() bool
}

func (self Layout) RefreshInterval() time.Duration {
	var fastest time.Duration

	for _, instances := range self {
		for _, instance := range instances {
			ticker, ok := instance.(Ticker)
			if !ok {
				continue
			}

			if interval := ticker.RefreshInterval(); interval > 0 && (fastest == 0 || interval < fastest) {
				fastest = interval
			}
		}
	}

	return fastest
}

func (self Layout) IdleRefreshInterval() time.Duration {
	var fastest time.Duration

	for _, instances := range self {
		for _, instance := range instances {
			persister, ok := instance.(Persister)
			if !ok || !persister.Persistent() {
				continue
			}

			if interval := persister.RefreshInterval(); interval > 0 && (fastest == 0 || interval < fastest) {
				fastest = interval
			}
		}
	}

	return fastest
}
