package subUsage

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/spinner"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/oh/usage"
	"crdx.org/io/internal/req"
)

var _ segment.Refresher = &state{}

const (
	defaultRate      = 5 * time.Minute
	redrawInterval   = 15 * time.Second
	spinnerDelay     = 250 * time.Millisecond
	firstFailureWait = time.Minute
	firstEmptyWait   = redrawInterval
	backoffFactor    = 2
	dayLength        = 24 * time.Hour
	percentCeiling   = 100
	overPacePercent  = 10
	overPaceRatio    = 1.5
	nearLimit        = 90
	scopeMark        = "⚡"
	limitedMark      = "✖"
	failureLabel     = "failed"
)

type snapshot struct {
	windows   []agent.UsageWindow
	fetchedAt time.Time
	status    usageStatus
	failure   string
}

type usageStatus int

const (
	usageReady usageStatus = iota
	usageFetching
	usagePending
	usageRetrying
)

type state struct {
	reporter  agent.UsageReporter
	modelName string
	rate      time.Duration
	now       func() time.Time

	mutex             sync.Mutex
	windows           []agent.UsageWindow
	fetchedAt         time.Time
	retryAt           time.Time
	fetchStartedAt    time.Time
	waited            time.Duration
	failure           string
	status            usageStatus
	statusBeforeFetch usageStatus
	shouldRedraw      bool
}

func New(reporter agent.UsageReporter, cachePath string, modelName string, now func() time.Time) segment.Factory {
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

		return &state{
			reporter:  usage.Shared(reporter, cachePath, args.Rate, now),
			modelName: strings.ToLower(modelName),
			rate:      args.Rate,
			now:       now,
			status:    usagePending,
		}, nil
	}
}

func (self *state) NextRefresh(phase segment.Phase) time.Time {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if self.shouldRedraw {
		return phase.At
	}

	if self.status == usageFetching {
		interval := spinner.Activity.RefreshInterval()

		return phase.At.Truncate(interval).Add(interval)
	}

	return phase.At.Truncate(redrawInterval).Add(redrawInterval)
}

func (self *state) Render(segment.Context) string {
	if self.reporter == nil || !self.reporter.IsAvailable() {
		return style.Dim("usage n/a")
	}

	self.mutex.Lock()
	shouldFetch := self.noteFetchStarting()
	current := snapshot{
		windows:   self.windows,
		fetchedAt: self.fetchedAt,
		status:    self.getVisibleStatus(),
		failure:   self.failure,
	}
	self.shouldRedraw = false
	self.mutex.Unlock()

	if shouldFetch {
		go self.fetch()
	}

	return self.draw(current)
}

func (self *state) getVisibleStatus() usageStatus {
	if self.status == usageFetching && self.now().Sub(self.fetchStartedAt) < spinnerDelay {
		return self.statusBeforeFetch
	}

	return self.status
}

func (self *state) noteFetchStarting() bool {
	now := self.now()

	if self.status == usageFetching || now.Before(self.retryAt) || now.Sub(self.fetchedAt) < self.rate {
		return false
	}

	self.fetchStartedAt = now
	self.statusBeforeFetch = self.status
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
		self.failure = failureReason(err)
		self.waitLonger(firstFailureWait)

		return
	}

	if len(windows) == 0 {
		self.status = usagePending
		self.waitLonger(firstEmptyWait)

		return
	}

	self.status = usageReady
	self.failure = ""
	self.windows = windows
	self.fetchedAt = self.now()
	self.retryAt = time.Time{}
	self.waited = 0
}

func (self *state) waitLonger(first time.Duration) {
	self.waited = min(max(self.waited*backoffFactor, first), self.rate)
	self.retryAt = self.now().Add(self.waited)
}

func (self *state) draw(current snapshot) string {
	var parts, scoped []string

	for _, window := range current.windows {
		if !self.governsThisSession(window) {
			continue
		}

		text := drawWindow(window, current.fetchedAt, self.now())

		if window.Scope == "" {
			parts = append(parts, text)
		} else {
			scoped = append(scoped, text)
		}
	}

	if len(scoped) > 0 {
		parts = append(parts, style.Subtle(scopeMark))
		parts = append(parts, scoped...)
	}

	usage := strings.Join(parts, " ")

	switch current.status {
	case usageFetching:
		return appendUsageStatus(usage, "usage", style.Spinner(self.spinnerFrame()))
	case usagePending:
		return appendUsageStatus(usage, "usage", style.Subtle("pending"))
	case usageRetrying:
		return appendUsageStatus(usage, "usage", style.Failure(current.failure))
	default:
		if usage == "" {
			return style.Dim("usage unavailable")
		}

		return usage
	}
}

func (self *state) governsThisSession(window agent.UsageWindow) bool {
	return window.Scope == "" || strings.Contains(self.modelName, window.Scope)
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
	actual := int(window.Percent + 0.5)
	text := fmt.Sprintf("%s %d%%", label, actual)

	if window.IsLimited {
		return style.Failure(text + " " + limitedMark)
	}

	if window.ResetsAt.IsZero() {
		return style.Quantity(text)
	}

	if !window.ResetsAt.After(now) {
		return style.Dim(label + " stale")
	}

	expected := expectedPercent(window, fetchedAt)

	switch classifyPace(actual, expected) {
	case paceAhead:
		return style.Change(text + " ▲")
	case paceCritical:
		return style.Failure(text + " ▲")
	default:
		return style.Quantity(text)
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

func failureReason(err error) string {
	if refused, ok := errors.AsType[*req.StatusError](err); ok {
		return strconv.Itoa(refused.Status)
	}

	return failureLabel
}
