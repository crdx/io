package subscriptionUsage_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/segment/subscriptionUsage"
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

func build(t *testing.T, reporter agent.UsageReporter) segment.Segment {
	t.Helper()

	built, err := subscriptionUsage.New(reporter, func() time.Time { return testNow })(noOptions{})
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

func TestAFailedFetchLeavesTheLineBlank(t *testing.T) {
	built := build(t, &scriptedReporter{err: errors.New("the endpoint is sulking")})

	if got := built.Render(segment.Context{}); got != "" {
		t.Errorf("expected nothing, got %q", got)
	}

	time.Sleep(10 * time.Millisecond)

	if got := built.Render(segment.Context{}); got != "" {
		t.Errorf("expected nothing after the failure, got %q", got)
	}
}
