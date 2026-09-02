package subUsage

import (
	"context"
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
)

var _ segment.Refresher = &state{}

const (
	defaultRate      = 5 * time.Minute
	redrawInterval   = 15 * time.Second
	spinnerDelay     = 250 * time.Millisecond
	firstFailureWait = time.Minute
	firstEmptyWait   = redrawInterval
	backoffFactor    = 2
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
	waitedTime        time.Duration
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

		sharedUsage := usage.Shared(reporter, cachePath, args.Rate, now)

		self := &state{
			reporter:  sharedUsage,
			modelName: strings.ToLower(modelName),
			rate:      args.Rate,
			now:       now,
			status:    usagePending,
		}

		self.startFromSnapshot(sharedUsage)

		return self, nil
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

func (self *state) startFromSnapshot(reporter agent.UsageReporter) {
	snapshotter, ok := reporter.(usage.Snapshotter)
	if !ok {
		return
	}

	windows, fetchedAt := snapshotter.GetSnapshot()
	if len(windows) == 0 || fetchedAt.IsZero() {
		return
	}

	self.windows = windows
	self.fetchedAt = fetchedAt
	self.status = usageReady
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
	self.waitedTime = 0
}

func (self *state) waitLonger(first time.Duration) {
	self.waitedTime = min(max(self.waitedTime*backoffFactor, first), self.rate)
	self.retryAt = self.now().Add(self.waitedTime)
}

func (self *state) draw(current snapshot) string {
	var parts, scopedParts []string

	for _, window := range current.windows {
		if !self.governsThisSession(window) {
			continue
		}

		text := drawWindow(window, current.fetchedAt, self.now())

		if window.Scope == "" {
			parts = append(parts, text)
		} else {
			scopedParts = append(scopedParts, text)
		}
	}

	if len(scopedParts) > 0 {
		parts = append(parts, style.Subtle(scopeMark))
		parts = append(parts, scopedParts...)
	}

	usage := strings.Join(parts, " ")

	switch current.status {
	case usageFetching:
		return appendUsageStatus(usage, "usage", style.Spinner(self.spinnerFrame()))
	case usagePending:
		return appendUsageStatus(usage, "usage", style.Subtle("pending"))
	case usageRetrying:
		return appendUsageStatus(usage, "usage", style.Failure(current.failure))
	case usageReady:
	}

	if usage == "" {
		return style.Dim("usage unavailable")
	}

	return usage
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
	label := usage.DurationLabel(window.Duration)
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

	expectedPercentage := usage.ExpectedPercent(window, fetchedAt)

	switch usage.ClassifyPace(actual, expectedPercentage) {
	case usage.PaceAhead:
		return style.Change(text + " ▲")
	case usage.PaceCritical:
		return style.Failure(text + " ▲")
	case usage.PaceEven:
	}

	return style.Quantity(text)
}

func failureReason(err error) string {
	if status, isRefusal := usage.FailureStatus(err); isRefusal {
		return strconv.Itoa(status)
	}

	return failureLabel
}
