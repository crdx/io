package jobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"crdx.org/io/internal/sandbox"
)

func TestWaitingOnAFinishedJobReturnsAtOnce(t *testing.T) {
	manager := New(nil)
	manager.Restore([]Snapshot{{Name: "build", Command: "just build", State: StateComplete}})

	if err := manager.Wait(t.Context(), "build"); err != nil {
		t.Errorf("got %v, want a finished job to be waited on for no time at all", err)
	}
}

func TestWaitingOnAnUnknownJobSaysSo(t *testing.T) {
	if err := New(nil).Wait(t.Context(), "ghost"); !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want the job not to be found", err)
	}
}

func TestWaitingReturnsAsSoonAsTheJobHasEnded(t *testing.T) {
	manager := New(nil)

	ending, err := manager.claim("docs", "python3", sandbox.Policy{})
	if err != nil {
		t.Fatal(err)
	}

	go func() {
		time.Sleep(50 * time.Millisecond)
		manager.conclude(ending, StateComplete, 0, "")
		close(ending.over)
	}()

	if err := manager.Wait(t.Context(), "docs"); err != nil {
		t.Fatalf("the wait failed: %v", err)
	}

	snapshot, err := manager.Status("docs")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.IsLive() {
		t.Errorf("got state %s, want the wait to have outlasted the job", snapshot.State)
	}
}

func TestWaitingEndsWithTheContextThatAskedForIt(t *testing.T) {
	manager := New(nil)

	if _, err := manager.claim("docs", "python3", sandbox.Policy{}); err != nil {
		t.Fatal(err)
	}

	waiting, stopWaiting := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer stopWaiting()

	if err := manager.Wait(waiting, "docs"); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("got %v, want the wait to end with its context", err)
	}
}

func TestWaitingOnAJobThatCouldNotBeStartedReturnsAtOnce(t *testing.T) {
	manager := New(refusingRunner{})

	if _, err := manager.Start(t.Context(), "docs", t.TempDir(), "python3", sandbox.Policy{}); err == nil {
		t.Fatal("the job started, want it refused")
	}

	waiting, stopWaiting := context.WithTimeout(t.Context(), time.Second)
	defer stopWaiting()

	if err := manager.Wait(waiting, "docs"); err != nil {
		t.Errorf("got %v, want a job that never ran to be waited on for no time at all", err)
	}
}

type refusingRunner struct{}

func (refusingRunner) Run(
	context.Context,
	string,
	string,
	sandbox.Policy,
) (sandbox.Result, error) {
	return sandbox.Result{}, errors.New("nothing runs here")
}

func (refusingRunner) Start(
	context.Context,
	string,
	string,
	sandbox.Policy,
	sandbox.Output,
) (sandbox.Command, error) {
	return nil, errors.New("nothing runs here")
}
