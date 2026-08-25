package subscriptionUsage

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/style"
)

var _ segment.Persister = &state{}

const (
	defaultRate     = 5 * time.Minute
	failureBackoff  = time.Minute
	redrawInterval  = 15 * time.Second
	dayLength       = 24 * time.Hour
	percentCeiling  = 100
	overPacePercent = 10
	overPaceRatio   = 1.5
	nearLimit       = 90
)

type state struct {
	reporter agent.UsageReporter
	rate     time.Duration
	now      func() time.Time

	mutex     sync.Mutex
	windows   []agent.UsageWindow
	fetchedAt time.Time
	retryAt   time.Time
	fetching  bool
}

// New builds the factory over whichever provider holds the conversation. A provider that cannot
// report subscription usage hands a nil reporter, and the segment stays blank rather than refusing
// the layout, so one layout serves every provider.
func New(reporter agent.UsageReporter, now func() time.Time) segment.Factory {
	return func(options segment.Options) (segment.Segment, error) {
		var args struct {
			Rate time.Duration `toml:"rate"`
		}

		if err := options.Read(&args); err != nil {
			return nil, err
		}

		if args.Rate < 0 {
			return nil, fmt.Errorf("rate is %s, and wants to be longer than nothing", args.Rate)
		}

		if args.Rate == 0 {
			args.Rate = defaultRate
		}

		return &state{reporter: reporter, rate: args.Rate, now: now}, nil
	}
}

func (self *state) RefreshInterval() time.Duration {
	return redrawInterval
}

func (self *state) Persistent() bool {
	return true
}

// Render paints the last snapshot and never waits on the network: a stale snapshot starts a fetch
// in the background, and what is already held is what this redraw shows.
func (self *state) Render(segment.Context) string {
	if self.reporter == nil {
		return ""
	}

	self.mutex.Lock()
	windows, fetchedAt := self.windows, self.fetchedAt
	shouldFetch := self.noteFetchStarting()
	self.mutex.Unlock()

	if shouldFetch {
		go self.fetch()
	}

	return self.draw(windows, fetchedAt)
}

func (self *state) noteFetchStarting() bool {
	now := self.now()

	if self.fetching || now.Before(self.retryAt) || now.Sub(self.fetchedAt) < self.rate {
		return false
	}

	self.fetching = true

	return true
}

func (self *state) fetch() {
	windows, err := self.reporter.UsageWindows(context.Background())

	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.fetching = false

	if err != nil {
		self.retryAt = self.now().Add(failureBackoff)

		return
	}

	self.windows = windows
	self.fetchedAt = self.now()
	self.retryAt = time.Time{}
}

func (self *state) draw(windows []agent.UsageWindow, fetchedAt time.Time) string {
	var parts []string

	for _, window := range windows {
		if window.Scope != "" {
			continue
		}

		parts = append(parts, drawWindow(window, fetchedAt, self.now()))
	}

	return strings.Join(parts, " ")
}

func drawWindow(window agent.UsageWindow, fetchedAt time.Time, now time.Time) string {
	label := durationLabel(window.Duration)

	if !window.ResetsAt.After(now) {
		return style.Faint(label + " idle")
	}

	actual := int(window.Percent + 0.5)
	expected := expectedPercent(window, fetchedAt)

	text := fmt.Sprintf("%s %d%%", label, actual)

	switch classifyPace(actual, expected) {
	case paceAhead:
		return style.Change(text + " ▲")
	case paceCritical:
		return style.Failure(text + " ▲")
	default:
		return style.Subtle(text)
	}
}

func expectedPercent(window agent.UsageWindow, at time.Time) int {
	start := window.ResetsAt.Add(-window.Duration)

	elapsed := at.Sub(start)
	if elapsed < 0 {
		return 0
	}

	percent := int(elapsed * percentCeiling / window.Duration)

	return min(percentCeiling, percent)
}

type pace int

const (
	paceEven pace = iota
	paceAhead
	paceCritical
)

func classifyPace(actual int, expected int) pace {
	if actual < overPacePercent || actual <= expected {
		return paceEven
	}

	if actual >= nearLimit || float64(actual) >= float64(expected)*overPaceRatio {
		return paceCritical
	}

	return paceAhead
}

func durationLabel(duration time.Duration) string {
	switch {
	case duration >= dayLength && duration%dayLength == 0:
		return fmt.Sprintf("%dd", duration/dayLength)
	case duration >= time.Hour && duration%time.Hour == 0:
		return fmt.Sprintf("%dh", duration/time.Hour)
	default:
		return fmt.Sprintf("%dm", duration/time.Minute)
	}
}
