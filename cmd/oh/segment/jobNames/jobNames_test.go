package jobNames_test

import (
	"testing"
	"time"

	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/segment/jobNames"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/internal/jobs"
)

func build(t *testing.T, listing ...jobs.Snapshot) segment.Segment {
	t.Helper()

	built, err := jobNames.New(func() []jobs.Snapshot { return listing })(nil)
	if err != nil {
		t.Fatalf("could not build the segment: %v", err)
	}

	return built
}

func TestNoJobsDrawNothing(t *testing.T) {
	if drawn := build(t).Render(segment.Context{}); drawn != "" {
		t.Errorf("got %q, want nothing at all", drawn)
	}
}

func TestEveryRunningJobIsNamed(t *testing.T) {
	drawn := style.Plain(build(t,
		jobs.Snapshot{Name: "docs", State: jobs.StateRunning},
		jobs.Snapshot{Name: "watch", State: jobs.StateRunning},
	).Render(segment.Context{}))

	if drawn != "\u25cf docs \u25cf watch" {
		t.Errorf("got %q, want both jobs named with their own mark", drawn)
	}
}

func TestAFailedJobIsNamedWithACross(t *testing.T) {
	drawn := style.Plain(build(t,
		jobs.Snapshot{Name: "builder", State: jobs.StateFailed},
	).Render(segment.Context{}))

	if drawn != "\u2717 builder" {
		t.Errorf("got %q, want the failure named", drawn)
	}
}

func TestAJobBeingStoppedIsDrawnAsAChange(t *testing.T) {
	stopping := build(t, jobs.Snapshot{Name: "docs", State: jobs.StateStopping}).Render(segment.Context{})
	running := build(t, jobs.Snapshot{Name: "docs", State: jobs.StateRunning}).Render(segment.Context{})

	if stopping == running {
		t.Error("a job being stopped drew the same as one running")
	}
	if stopping != style.Change("\u25cf docs") {
		t.Errorf("got %q, want the change style", stopping)
	}
}

func TestAFinishedJobLingersAsAGhost(t *testing.T) {
	for _, state := range []jobs.State{jobs.StateComplete, jobs.StateStopped, jobs.StateEnded} {
		drawn := build(t, jobs.Snapshot{Name: "docs", State: state}).Render(segment.Context{})
		if plain := style.Plain(drawn); plain != "\u25cb docs" {
			t.Errorf("state %s drew %q, want a hollow ghost", state, plain)
		}
		if drawn != style.Dim("\u25cb docs") {
			t.Errorf("state %s drew %q, want it held back in dim", state, drawn)
		}
	}
}

func TestAGhostAsksForNoRedraw(t *testing.T) {
	refresher, _ := build(t, jobs.Snapshot{Name: "docs", State: jobs.StateEnded}).(segment.Refresher)
	if at := refresher.NextRefresh(segment.Phase{At: time.Now()}); !at.IsZero() {
		t.Errorf("got %v, want a ghost to ask for no redraw", at)
	}
}

func TestALiveJobAsksForARedraw(t *testing.T) {
	refresher, isRefresher := build(t, jobs.Snapshot{Name: "docs", State: jobs.StateRunning}).(segment.Refresher)
	if !isRefresher {
		t.Fatal("the segment does not ask for a redraw")
	}

	if at := refresher.NextRefresh(segment.Phase{At: time.Now()}); at.IsZero() {
		t.Error("a running job did not ask for a redraw")
	}
}

func TestNothingLiveAsksForNoRedraw(t *testing.T) {
	refresher, isRefresher := build(t, jobs.Snapshot{Name: "builder", State: jobs.StateFailed}).(segment.Refresher)
	if !isRefresher {
		t.Fatal("the segment does not ask for a redraw")
	}

	if at := refresher.NextRefresh(segment.Phase{At: time.Now()}); !at.IsZero() {
		t.Errorf("got %v, want nothing live to ask for no redraw", at)
	}
}
