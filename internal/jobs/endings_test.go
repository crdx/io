package jobs

import (
	"context"
	"syscall"
	"testing"
	"time"

	"crdx.org/io/internal/sandbox"
)

type heldRunner struct {
	release chan struct{}
	result  sandbox.Result
}

func (self *heldRunner) Run(context.Context, string, string, sandbox.Policy) (sandbox.Result, error) {
	return self.result, nil
}

func (self *heldRunner) Start(
	context.Context,
	string,
	string,
	sandbox.Policy,
	sandbox.Output,
) (sandbox.Command, error) {
	return &heldCommand{runner: self}, nil
}

type heldCommand struct {
	runner *heldRunner
}

func (self *heldCommand) Wait() (sandbox.Result, error) {
	<-self.runner.release

	return self.runner.result, nil
}

func (self *heldCommand) Signal(syscall.Signal) error {
	close(self.runner.release)

	return nil
}

func (self *heldCommand) Stop() {}

func newHeldRunner(result sandbox.Result) *heldRunner {
	return &heldRunner{release: make(chan struct{}), result: result}
}

func nextEnding(t *testing.T, manager *Manager) (Snapshot, bool) {
	t.Helper()

	select {
	case snapshot := <-manager.Endings():
		return snapshot, true
	case <-time.After(time.Second):
		return Snapshot{}, false
	}
}

func TestAJobThatEndsOnItsOwnAnnouncesItself(t *testing.T) {
	runner := newHeldRunner(sandbox.Result{ExitCode: 2})
	manager := New(runner)

	if _, err := manager.Start(t.Context(), "build", t.TempDir(), "just build", sandbox.Policy{}); err != nil {
		t.Fatal(err)
	}

	close(runner.release)

	snapshot, isAnnounced := nextEnding(t, manager)
	if !isAnnounced {
		t.Fatal("a job that ended on its own announced nothing")
	}
	if snapshot.Name != "build" || snapshot.State != StateFailed || snapshot.ExitCode != 2 {
		t.Errorf("got %#v, want the failed job", snapshot)
	}
}

func TestAJobStoppedFromTheKeyboardAnnouncesNothing(t *testing.T) {
	runner := newHeldRunner(sandbox.Result{})
	manager := New(runner)

	if _, err := manager.Start(t.Context(), "docs", t.TempDir(), "python3", sandbox.Policy{}); err != nil {
		t.Fatal(err)
	}

	if _, err := manager.Stop("docs"); err != nil {
		t.Fatal(err)
	}

	select {
	case snapshot := <-manager.Endings():
		t.Errorf("got %#v, want a stopped job to announce nothing", snapshot)
	default:
	}
}

func TestAJobBeingWaitedOnAnnouncesNothing(t *testing.T) {
	runner := newHeldRunner(sandbox.Result{})
	manager := New(runner)

	if _, err := manager.Start(t.Context(), "build", t.TempDir(), "just build", sandbox.Policy{}); err != nil {
		t.Fatal(err)
	}

	waiting := make(chan struct{})
	go func() {
		defer close(waiting)
		if _, err := manager.Wait(t.Context(), []string{"build"}); err != nil {
			t.Error(err)
		}
	}()

	time.Sleep(50 * time.Millisecond)
	close(runner.release)
	<-waiting

	select {
	case snapshot := <-manager.Endings():
		t.Errorf("got %#v, want a job that was waited on to announce nothing", snapshot)
	default:
	}
}

func TestAJobThatEndsAfterAWaitGaveUpAnnouncesItself(t *testing.T) {
	runner := newHeldRunner(sandbox.Result{})
	manager := New(runner)

	if _, err := manager.Start(t.Context(), "build", t.TempDir(), "just build", sandbox.Policy{}); err != nil {
		t.Fatal(err)
	}

	waiting, giveUp := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer giveUp()

	if _, err := manager.Wait(waiting, []string{"build"}); err == nil {
		t.Fatal("the wait returned before the job ended")
	}

	close(runner.release)

	snapshot, isAnnounced := nextEnding(t, manager)
	if !isAnnounced {
		t.Fatal("a job that ended after its wait gave up announced nothing")
	}
	if snapshot.Name != "build" {
		t.Errorf("got %#v, want the job that ended", snapshot)
	}
}
