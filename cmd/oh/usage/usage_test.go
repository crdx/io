package usage_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/usage"
)

const rate = 5 * time.Minute

var testNow = time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)

type scriptedReporter struct {
	windows     []agent.UsageWindow
	err         error
	isAvailable bool
	asked       atomic.Int64
}

func (self *scriptedReporter) IsAvailable() bool {
	return self.isAvailable
}

func (self *scriptedReporter) UsageWindows(context.Context) ([]agent.UsageWindow, error) {
	self.asked.Add(1)

	return self.windows, self.err
}

type testClock struct {
	now time.Time
}

func (self *testClock) read() time.Time {
	return self.now
}

func (self *testClock) set(now time.Time) {
	self.now = now
}

func windows(percent float64) []agent.UsageWindow {
	return []agent.UsageWindow{{Duration: 5 * time.Hour, Percent: percent, ResetsAt: testNow}}
}

func cachePath(t *testing.T) string {
	t.Helper()

	return filepath.Join(t.TempDir(), "usage", "codex.json")
}

func TestAFreshSnapshotIsServedWithoutAskingTheProvider(t *testing.T) {
	path := cachePath(t)
	clock := &testClock{now: testNow}

	first := &scriptedReporter{windows: windows(40), isAvailable: true}
	if _, err := usage.Shared(first, path, rate, clock.read).UsageWindows(t.Context()); err != nil {
		t.Fatal(err)
	}

	second := &scriptedReporter{windows: windows(99), isAvailable: true}
	got, err := usage.Shared(second, path, rate, clock.read).UsageWindows(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if asked := second.asked.Load(); asked != 0 {
		t.Errorf("the second session asked its provider %d times", asked)
	}

	if len(got) != 1 || got[0].Percent != 40 {
		t.Errorf("expected the shared snapshot, got %v", got)
	}
}

func TestAStaleSnapshotIsRefreshed(t *testing.T) {
	path := cachePath(t)
	clock := &testClock{now: testNow}

	first := &scriptedReporter{windows: windows(40), isAvailable: true}
	if _, err := usage.Shared(first, path, rate, clock.read).UsageWindows(t.Context()); err != nil {
		t.Fatal(err)
	}

	clock.set(testNow.Add(rate))

	second := &scriptedReporter{windows: windows(99), isAvailable: true}
	got, err := usage.Shared(second, path, rate, clock.read).UsageWindows(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if asked := second.asked.Load(); asked != 1 {
		t.Errorf("the second session asked its provider %d times", asked)
	}

	if len(got) != 1 || got[0].Percent != 99 {
		t.Errorf("expected the refreshed snapshot, got %v", got)
	}
}

func TestASessionWithNothingToSayStartsFromTheSharedSnapshot(t *testing.T) {
	path := cachePath(t)
	clock := &testClock{now: testNow}

	first := &scriptedReporter{windows: windows(40), isAvailable: true}
	if _, err := usage.Shared(first, path, rate, clock.read).UsageWindows(t.Context()); err != nil {
		t.Fatal(err)
	}

	beforeItsFirstTurn := &scriptedReporter{isAvailable: false}
	shared := usage.Shared(beforeItsFirstTurn, path, rate, clock.read)

	if !shared.IsAvailable() {
		t.Fatal("expected the shared snapshot to stand in until the first turn lands")
	}

	got, err := shared.UsageWindows(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 1 || got[0].Percent != 40 {
		t.Errorf("expected the shared snapshot, got %v", got)
	}
}

func TestAnEmptyAnswerLeavesTheSharedSnapshotStanding(t *testing.T) {
	path := cachePath(t)
	clock := &testClock{now: testNow}

	first := &scriptedReporter{windows: windows(40), isAvailable: true}
	if _, err := usage.Shared(first, path, rate, clock.read).UsageWindows(t.Context()); err != nil {
		t.Fatal(err)
	}

	clock.set(testNow.Add(rate))

	silent := &scriptedReporter{isAvailable: true}
	got, err := usage.Shared(silent, path, rate, clock.read).UsageWindows(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 1 || got[0].Percent != 40 {
		t.Errorf("expected the last snapshot kept, got %v", got)
	}
}

func TestARefusedRefreshIsReportedRatherThanSwallowed(t *testing.T) {
	path := cachePath(t)
	clock := &testClock{now: testNow}

	refused := &scriptedReporter{err: errors.New("the endpoint is sulking"), isAvailable: true}

	if _, err := usage.Shared(refused, path, rate, clock.read).UsageWindows(t.Context()); err == nil {
		t.Error("expected the refusal reported")
	}
}

func TestNowhereToShareLeavesTheReporterAlone(t *testing.T) {
	reporter := &scriptedReporter{windows: windows(40), isAvailable: true}

	if shared := usage.Shared(reporter, "", rate, testClockRead); shared != reporter {
		t.Errorf("expected the reporter handed back untouched, got %T", shared)
	}

	if shared := usage.Shared(nil, "somewhere", rate, testClockRead); shared != nil {
		t.Errorf("expected nothing to wrap, got %T", shared)
	}
}

func TestASessionAlreadyRefreshingIsNotQueuedBehind(t *testing.T) {
	path := cachePath(t)
	clock := &testClock{now: testNow}

	first := &scriptedReporter{windows: windows(40), isAvailable: true}
	if _, err := usage.Shared(first, path, rate, clock.read).UsageWindows(t.Context()); err != nil {
		t.Fatal(err)
	}

	clock.set(testNow.Add(rate))
	release := holdTheLock(t, path)
	defer release()

	second := &scriptedReporter{windows: windows(99), isAvailable: true}
	got, err := usage.Shared(second, path, rate, clock.read).UsageWindows(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if asked := second.asked.Load(); asked != 0 {
		t.Errorf("the waiting session asked its provider %d times", asked)
	}

	if len(got) != 1 || got[0].Percent != 40 {
		t.Errorf("expected what is written, got %v", got)
	}
}

func holdTheLock(t *testing.T, path string) func() {
	t.Helper()

	file, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}

	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}

	return func() {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
	}
}

func testClockRead() time.Time {
	return testNow
}

func TestOnlyOneOfManySessionsRefreshesAtATime(t *testing.T) {
	path := cachePath(t)
	clock := &testClock{now: testNow}

	reporters := make([]*scriptedReporter, 8)
	shared := make([]agent.UsageReporter, len(reporters))

	for at := range reporters {
		reporters[at] = &scriptedReporter{windows: windows(float64(at)), isAvailable: true}
		shared[at] = usage.Shared(reporters[at], path, rate, clock.read)
	}

	var waiting sync.WaitGroup

	for _, reporter := range shared {
		waiting.Go(func() {
			if _, err := reporter.UsageWindows(t.Context()); err != nil {
				t.Error(err)
			}
		})
	}

	waiting.Wait()

	asked := 0

	for _, reporter := range reporters {
		asked += int(reporter.asked.Load())
	}

	if asked != 1 {
		t.Errorf("expected one session to refresh for all of them, %d did", asked)
	}
}

func TestASnapshotFromANewerOhIsRefused(t *testing.T) {
	path := cachePath(t)

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(`{"version":99,"windows":"not a list now"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	reporter := &scriptedReporter{windows: windows(40), isAvailable: true}
	shared := usage.Shared(reporter, path, rate, testClockRead)

	if _, err := shared.UsageWindows(t.Context()); err == nil {
		t.Error("expected a snapshot from a newer build to be named rather than read")
	}
}

func TestAnUnreadableSnapshotIsRefreshedRatherThanFatal(t *testing.T) {
	path := cachePath(t)

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte("not json at all"), 0o600); err != nil {
		t.Fatal(err)
	}

	reporter := &scriptedReporter{windows: windows(40), isAvailable: true}
	shared := usage.Shared(reporter, path, rate, testClockRead)

	if shared.IsAvailable() != true {
		t.Error("expected the reporter's own availability to stand")
	}

	if _, err := shared.UsageWindows(t.Context()); err == nil {
		t.Error("expected the unreadable snapshot named")
	}
}

func TestASnapshotIsFreshUpToTheMomentItIsNot(t *testing.T) {
	path := cachePath(t)
	clock := &testClock{now: testNow}

	first := &scriptedReporter{windows: windows(40), isAvailable: true}
	if _, err := usage.Shared(first, path, rate, clock.read).UsageWindows(t.Context()); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name     string
		at       time.Time
		wantAsks int64
	}{
		{name: "a moment before the rate", at: testNow.Add(rate - time.Nanosecond)},
		{name: "the moment of the rate", at: testNow.Add(rate), wantAsks: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			clock.set(test.at)

			second := &scriptedReporter{windows: windows(99), isAvailable: true}
			if _, err := usage.Shared(second, path, rate, clock.read).UsageWindows(t.Context()); err != nil {
				t.Fatal(err)
			}

			if asked := second.asked.Load(); asked != test.wantAsks {
				t.Errorf("asked %d times, want %d", asked, test.wantAsks)
			}
		})
	}
}

func TestTheSnapshotOutlivesTheSessionThatTookIt(t *testing.T) {
	path := cachePath(t)
	clock := &testClock{now: testNow}

	first := &scriptedReporter{
		windows: []agent.UsageWindow{{
			Duration:  5 * time.Hour,
			Percent:   40,
			ResetsAt:  testNow.Add(time.Hour),
			Scope:     "gpt-5.3-codex-spark",
			IsLimited: true,
		}},
		isAvailable: true,
	}

	if _, err := usage.Shared(first, path, rate, clock.read).UsageWindows(t.Context()); err != nil {
		t.Fatal(err)
	}

	windows, err := usage.Shared(
		&scriptedReporter{isAvailable: false}, path, rate, clock.read,
	).UsageWindows(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	want := agent.UsageWindow{
		Duration:  5 * time.Hour,
		Percent:   40,
		ResetsAt:  testNow.Add(time.Hour),
		Scope:     "gpt-5.3-codex-spark",
		IsLimited: true,
	}

	if len(windows) != 1 || !windows[0].ResetsAt.Equal(want.ResetsAt) {
		t.Fatalf("got %+v", windows)
	}

	windows[0].ResetsAt = want.ResetsAt

	if windows[0] != want {
		t.Errorf("got %+v, want %+v", windows[0], want)
	}
}

func TestASnapshotIsHandedBackWithoutAskingTheProvider(t *testing.T) {
	path := cachePath(t)
	clock := &testClock{now: testNow}

	first := &scriptedReporter{windows: windows(40), isAvailable: true}
	if _, err := usage.Shared(first, path, rate, clock.read).UsageWindows(t.Context()); err != nil {
		t.Fatal(err)
	}

	second := &scriptedReporter{isAvailable: true}

	snapshotter, ok := usage.Shared(second, path, rate, clock.read).(usage.Snapshotter)
	if !ok {
		t.Fatal("expected a shared reporter to hand back its snapshot")
	}

	got, fetchedAt := snapshotter.GetSnapshot()
	if len(got) != 1 || got[0].Percent != 40 {
		t.Errorf("got %v", got)
	}

	if !fetchedAt.Equal(testNow) {
		t.Errorf("fetched at %s, want %s", fetchedAt, testNow)
	}

	if asked := second.asked.Load(); asked != 0 {
		t.Errorf("the provider was asked %d times", asked)
	}
}
