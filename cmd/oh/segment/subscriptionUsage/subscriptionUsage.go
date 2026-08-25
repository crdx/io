package subscriptionUsage

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/spinner"
	"crdx.org/io/cmd/oh/style"
)

var (
	_ segment.IdleTicker = &state{}
	_ segment.Persister  = &state{}
)

const (
	defaultRate      = 5 * time.Minute
	redrawInterval   = 15 * time.Second
	firstFailureWait = time.Minute
	firstEmptyWait   = redrawInterval
	backoffFactor    = 2
	dayLength        = 24 * time.Hour
	percentCeiling   = 100
	overPacePercent  = 10
	overPaceRatio    = 1.5
	nearLimit        = 90
)

type usageStatus int

const (
	usageReady usageStatus = iota
	usageFetching
	usagePending
	usageRetrying
)

type state struct {
	reporter agent.UsageReporter
	rate     time.Duration
	now      func() time.Time

	mutex        sync.Mutex
	windows      []agent.UsageWindow
	fetchedAt    time.Time
	retryAt      time.Time
	waited       time.Duration
	status       usageStatus
	shouldRedraw bool
}

// New builds the factory over whichever provider holds the conversation. A provider that cannot
// report subscription usage hands a nil or unavailable reporter, and the segment says n/a rather
// than refusing the layout, so one layout serves every provider. A reporter may also answer with
// nothing to say rather than an error, since codex has no address to poll and knows nothing until
// its first turn answers, and the first ask arrives at startup long before that. An empty answer
// is not a snapshot, so a short backoff holds the next ask rather than the whole rate period,
// which would leave the line blank for minutes after a startup that asked too early. That backoff
// doubles each time the answer disappoints, up to the rate, so a reporter that will shortly have
// something to say is heard almost at once while one that never will is left alone: anthropic's
// usage address is undocumented and unbudgeted, and answers a caller that leans on it with 429s.
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
	return spinner.Activity.RefreshInterval()
}

func (self *state) IdleRefreshInterval() time.Duration {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if self.status == usageFetching || self.shouldRedraw {
		return spinner.Activity.RefreshInterval()
	}

	return redrawInterval
}

func (self *state) Persistent() bool {
	return true
}

// Render paints the last snapshot and never waits on the network: a stale snapshot starts a fetch
// in the background, and what is already held is what this redraw shows.
func (self *state) Render(segment.Context) string {
	if self.reporter == nil || !self.reporter.IsAvailable() {
		return style.Withheld("usage n/a")
	}

	self.mutex.Lock()
	shouldFetch := self.noteFetchStarting()
	windows, fetchedAt, status := self.windows, self.fetchedAt, self.status
	self.shouldRedraw = false
	self.mutex.Unlock()

	if shouldFetch {
		go self.fetch()
	}

	return self.draw(windows, fetchedAt, status)
}

func (self *state) noteFetchStarting() bool {
	now := self.now()

	if self.status == usageFetching || now.Before(self.retryAt) || now.Sub(self.fetchedAt) < self.rate {
		return false
	}

	self.status = usageFetching

	return true
}

func (self *state) fetch() {
	windows, err := self.reporter.UsageWindows(context.Background())

	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.shouldRedraw = true

	if err != nil {
		self.status = usageRetrying
		self.waitLonger(firstFailureWait)

		return
	}

	if len(windows) == 0 {
		self.status = usagePending
		self.waitLonger(firstEmptyWait)

		return
	}

	self.status = usageReady
	self.windows = windows
	self.fetchedAt = self.now()
	self.retryAt = time.Time{}
	self.waited = 0
}

func (self *state) waitLonger(first time.Duration) {
	self.waited = min(max(self.waited*backoffFactor, first), self.rate)
	self.retryAt = self.now().Add(self.waited)
}

func (self *state) draw(windows []agent.UsageWindow, fetchedAt time.Time, status usageStatus) string {
	var parts []string

	for _, window := range windows {
		if window.Scope != "" {
			continue
		}

		parts = append(parts, drawWindow(window, fetchedAt, self.now()))
	}

	usage := strings.Join(parts, " ")

	switch status {
	case usageFetching:
		return appendUsageStatus(usage, "usage", style.Spinner(self.spinnerFrame()))
	case usagePending:
		return appendUsageStatus(usage, "usage", style.Subtle("pending"))
	case usageRetrying:
		return appendUsageStatus(usage, "usage", style.Subtle("retrying"))
	default:
		if usage == "" {
			return style.Withheld("usage unavailable")
		}

		return usage
	}
}

func (self *state) spinnerFrame() string {
	interval := spinner.Activity.RefreshInterval()
	frameIndex := int(self.now().UnixNano() / interval.Nanoseconds())

	return spinner.Activity.Frame(frameIndex)
}

func appendUsageStatus(usage string, emptyLabel string, status string) string {
	if usage == "" {
		return emptyLabel + " " + status
	}

	return usage + " " + status
}

func drawWindow(window agent.UsageWindow, fetchedAt time.Time, now time.Time) string {
	label := durationLabel(window.Duration)

	if !window.ResetsAt.After(now) {
		return style.Faint(label + " stale")
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
