package subscriptionUsage

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/style"
)

type noOptions struct{}

func (noOptions) Read(any) error { return nil }

type scriptedReporter struct {
	windows []agent.UsageWindow
	err     error
	asked   atomic.Int64
}

func (self *scriptedReporter) UsageWindows(context.Context) ([]agent.UsageWindow, error) {
	self.asked.Add(1)

	return self.windows, self.err
}

var testNow = time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)

type testClock struct {
	now time.Time
}

func (self *testClock) read() time.Time {
	return self.now
}

func (self *testClock) set(now time.Time) {
	self.now = now
}

func build(t *testing.T, reporter agent.UsageReporter) segment.Segment {
	t.Helper()

	return buildOnClock(t, reporter, &testClock{now: testNow})
}

func buildOnClock(t *testing.T, reporter agent.UsageReporter, clock *testClock) segment.Segment {
	t.Helper()

	built, err := New(reporter, clock.read)(noOptions{})
	if err != nil {
		t.Fatal(err)
	}

	return built
}

func renderSettled(t *testing.T, built segment.Segment) string {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)

	for {
		if text := style.Plain(built.Render(segment.Context{})); text != "" {
			return text
		}

		if time.Now().After(deadline) {
			t.Fatal("the fetch never landed")
		}

		time.Sleep(time.Millisecond)
	}
}

func TestANilReporterRendersNothing(t *testing.T) {
	built := build(t, nil)

	if got := built.Render(segment.Context{}); got != "" {
		t.Errorf("expected nothing, got %q", got)
	}
}

func TestWindowsOnAnEvenBurnRenderPlainPercentages(t *testing.T) {
	built := build(t, &scriptedReporter{windows: []agent.UsageWindow{
		{
			Duration: 5 * time.Hour,
			Percent:  40,
			ResetsAt: testNow.Add(150 * time.Minute),
		},
		{
			Duration: 7 * 24 * time.Hour,
			Percent:  12,
			ResetsAt: testNow.Add(6 * 24 * time.Hour),
		},
	}})

	if got := renderSettled(t, built); got != "5h 40% 7d 12%" {
		t.Errorf("got %q", got)
	}
}

func TestAWindowOverPaceIsMarked(t *testing.T) {
	built := build(t, &scriptedReporter{windows: []agent.UsageWindow{{
		Duration: 5 * time.Hour,
		Percent:  28,
		ResetsAt: testNow.Add(4 * time.Hour),
	}}})

	if got := renderSettled(t, built); got != "5h 28% ▲" {
		t.Errorf("got %q", got)
	}
}

func TestAScopedWindowStaysOutOfTheLine(t *testing.T) {
	built := build(t, &scriptedReporter{windows: []agent.UsageWindow{
		{Duration: 5 * time.Hour, Percent: 40, ResetsAt: testNow.Add(150 * time.Minute)},
		{
			Duration: 7 * 24 * time.Hour,
			Percent:  3,
			ResetsAt: testNow.Add(6 * 24 * time.Hour),
			Scope:    "opus",
		},
	}})

	if got := renderSettled(t, built); got != "5h 40%" {
		t.Errorf("got %q", got)
	}
}

func TestAnExpiredWindowReadsAsIdle(t *testing.T) {
	built := build(t, &scriptedReporter{windows: []agent.UsageWindow{{
		Duration: 5 * time.Hour,
		Percent:  68,
		ResetsAt: testNow.Add(-time.Minute),
	}}})

	if got := renderSettled(t, built); got != "5h idle" {
		t.Errorf("got %q", got)
	}
}

func TestAFreshSnapshotIsNotFetchedAgain(t *testing.T) {
	reporter := &scriptedReporter{windows: []agent.UsageWindow{{
		Duration: 5 * time.Hour,
		Percent:  40,
		ResetsAt: testNow.Add(150 * time.Minute),
	}}}

	built := build(t, reporter)

	renderSettled(t, built)
	renderSettled(t, built)

	if asked := reporter.asked.Load(); asked != 1 {
		t.Errorf("expected one fetch, got %d", asked)
	}
}

func TestEmptyReportsBackOffToTheConfiguredRate(t *testing.T) {
	reporter := &scriptedReporter{}
	clock := &testClock{now: testNow}
	built := buildState(t, reporter, clock)

	fetchNow(t, built)

	waits := []time.Duration{
		15 * time.Second,
		30 * time.Second,
		time.Minute,
		2 * time.Minute,
		4 * time.Minute,
		5 * time.Minute,
		5 * time.Minute,
	}

	for _, wait := range waits {
		retryAt := clock.read().Add(wait)
		assertFetchStarts(t, built, clock, retryAt.Add(-time.Nanosecond), false)
		assertFetchStarts(t, built, clock, retryAt, true)
		built.fetch()
	}
}

func TestFailuresBackOffToTheConfiguredRate(t *testing.T) {
	reporter := &scriptedReporter{err: errors.New("the endpoint is sulking")}
	clock := &testClock{now: testNow}
	built := buildState(t, reporter, clock)

	fetchNow(t, built)

	waits := []time.Duration{
		time.Minute,
		2 * time.Minute,
		4 * time.Minute,
		5 * time.Minute,
		5 * time.Minute,
	}

	for _, wait := range waits {
		retryAt := clock.read().Add(wait)
		assertFetchStarts(t, built, clock, retryAt.Add(-time.Nanosecond), false)
		assertFetchStarts(t, built, clock, retryAt, true)
		built.fetch()
	}
}

func TestASuccessResetsTheEmptyReportBackoff(t *testing.T) {
	reporter := &scriptedReporter{}
	clock := &testClock{now: testNow}
	built := buildState(t, reporter, clock)

	fetchNow(t, built)

	firstRetryAt := testNow.Add(firstEmptyWait)
	reporter.windows = []agent.UsageWindow{{
		Duration: 5 * time.Hour,
		Percent:  40,
		ResetsAt: testNow.Add(150 * time.Minute),
	}}
	assertFetchStarts(t, built, clock, firstRetryAt, true)
	built.fetch()

	reporter.windows = nil
	afterSnapshot := firstRetryAt.Add(defaultRate)
	assertFetchStarts(t, built, clock, afterSnapshot, true)
	built.fetch()

	retryAt := afterSnapshot.Add(firstEmptyWait)
	assertFetchStarts(t, built, clock, retryAt.Add(-time.Nanosecond), false)
	assertFetchStarts(t, built, clock, retryAt, true)
}

func buildState(t *testing.T, reporter agent.UsageReporter, clock *testClock) *state {
	t.Helper()

	built := buildOnClock(t, reporter, clock)
	internal, ok := built.(*state)
	if !ok {
		t.Fatalf("built %T, want *state", built)
	}

	return internal
}

func fetchNow(t *testing.T, built *state) {
	t.Helper()

	if !built.noteFetchStarting() {
		t.Fatal("initial fetch did not start")
	}

	built.fetch()
}

func assertFetchStarts(t *testing.T, built *state, clock *testClock, now time.Time, want bool) {
	t.Helper()

	clock.set(now)

	if got := built.noteFetchStarting(); got != want {
		t.Errorf("at %s fetch start = %t, want %t", now.Sub(testNow), got, want)
	}
}

func TestAFailedFetchLeavesTheLineBlank(t *testing.T) {
	clock := &testClock{now: testNow}
	built := buildState(t, &scriptedReporter{err: errors.New("the endpoint is sulking")}, clock)

	fetchNow(t, built)

	if got := built.Render(segment.Context{}); got != "" {
		t.Errorf("expected nothing after the failure, got %q", got)
	}
}
