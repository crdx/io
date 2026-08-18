package main

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"crdx.org/io/internal/file"
)

var (
	nowReadOnly      = workspaceIs(false)
	nowReadWrite     = workspaceIs(true)
	gitReadOnly      = historyIs(false)
	gitWritable      = historyIs(true)
	backgroundOn     = backgroundIs(true)
	backgroundKilled = backgroundIs(false)
	shellGranted     = shellIs(true)
	shellWithheld    = shellIs(false)
)

func TestEveryClauseSaysSomethingAndSaysItBothWays(t *testing.T) {
	for name, clauses := range map[string][2]string{
		"workspace":  {nowReadOnly, nowReadWrite},
		"history":    {gitReadOnly, gitWritable},
		"background": {backgroundKilled, backgroundOn},
		"shell":      {shellWithheld, shellGranted},
	} {
		if clauses[0] == "" || clauses[1] == "" {
			t.Errorf("%s: expected a clause either way, got %q and %q", name, clauses[0], clauses[1])
		}

		if clauses[0] == clauses[1] {
			t.Errorf("%s: expected the two ways to read differently, both got %q", name, clauses[0])
		}
	}
}

func writable() caps { return capRead | capWrite }

func TestAFreshConversationSaysNothingAboutTheModeItOpenedIn(t *testing.T) {
	for _, initialCaps := range []caps{capRead, capRead | capWrite, capRead | capGit, capRead | capWrite | capGit} {
		if got := NewMode(initialCaps).Inject(); got != "" {
			t.Errorf("expected nothing to be announced, got %q", got)
		}
	}
}

func TestASwappedModeIsAnnouncedOnceAndOnlyOnce(t *testing.T) {
	self := NewMode(writable())
	self.Toggle(capWrite)

	if got := self.Inject(); got != nowReadOnly {
		t.Errorf("expected %q, got %q", nowReadOnly, got)
	}

	if got := self.Inject(); got != "" {
		t.Errorf("expected nothing the second time, got %q", got)
	}

	self.Toggle(capWrite)

	if got := self.Inject(); got != nowReadWrite {
		t.Errorf("expected %q, got %q", nowReadWrite, got)
	}
}

func TestBackgroundModeIsAnnouncedOnItsOwn(t *testing.T) {
	self := NewMode(writable())
	self.Toggle(capBackground)

	if got := self.Inject(); got != backgroundOn {
		t.Errorf("expected %q, got %q", backgroundOn, got)
	}

	self.Toggle(capBackground)
	if got := self.Inject(); got != backgroundKilled {
		t.Errorf("expected %q, got %q", backgroundKilled, got)
	}
}

func TestOpeningTheHistoryIsAnnouncedOnItsOwn(t *testing.T) {
	self := NewMode(writable())
	self.Toggle(capGit)

	if got := self.Inject(); got != gitWritable {
		t.Errorf("expected %q, got %q", gitWritable, got)
	}

	self.Toggle(capGit)

	if got := self.Inject(); got != gitReadOnly {
		t.Errorf("expected %q, got %q", gitReadOnly, got)
	}
}

func TestBothSwapsBetweenTurnsAreAnnouncedTogether(t *testing.T) {
	self := NewMode(writable())

	self.Toggle(capWrite)
	self.Toggle(capGit)

	if got := self.Inject(); got != nowReadOnly+" "+gitWritable {
		t.Errorf("expected both clauses, got %q", got)
	}
}

func TestAModeSwappedTwiceIsNotAnnounced(t *testing.T) {
	self := NewMode(writable())

	self.Toggle(capWrite)
	self.Toggle(capWrite)

	if got := self.Inject(); got != "" {
		t.Errorf("expected nothing to be announced, got %q", got)
	}
}

func TestAResumedConversationAlwaysSaysWhatTheModeAllows(t *testing.T) {
	got := NewResumedMode(writable()).Inject()

	for _, clause := range []string{nowReadWrite, gitReadOnly, backgroundKilled, shellWithheld} {
		if !strings.Contains(got, clause) {
			t.Errorf("expected %q in %q", clause, got)
		}
	}
}

func TestSwitchingTheShellIsAnnounced(t *testing.T) {
	self := NewMode(capRead | capShell)
	self.Toggle(capShell)

	if got := self.Inject(); got != shellWithheld {
		t.Errorf("expected %q, got %q", shellWithheld, got)
	}

	self.Toggle(capShell)

	if got := self.Inject(); got != shellGranted {
		t.Errorf("expected %q, got %q", shellGranted, got)
	}
}

func TestTheModeSaysWhatTheWorkspaceAllows(t *testing.T) {
	self := NewMode(writable())

	if currentCaps := self.Current(); !currentCaps.has(capWrite) || currentCaps.has(capGit) {
		t.Error("expected a mode opened writable to say so")
	}

	self.Toggle(capWrite)
	self.Toggle(capGit)

	if currentCaps := self.Current(); currentCaps.has(capWrite) || !currentCaps.has(capGit) {
		t.Error("expected a commit-only mode to say so")
	}
}

func TestTheRuleAnswersForBothHalvesOfTheMode(t *testing.T) {
	for name, want := range map[string]struct {
		currentCaps caps
		workspace   error
		history     error
	}{
		"nothing":     {capRead, file.ErrReadOnly, file.ErrGitDir},
		"the lot":     {capRead | capWrite | capGit, nil, nil},
		"the tree":    {capRead | capWrite, nil, file.ErrGitDir},
		"commit only": {capRead | capGit, file.ErrReadOnly, nil},
	} {
		refuse := refuseWrite(NewMode(want.currentCaps))

		if got := refuse("main.go"); !errors.Is(got, want.workspace) {
			t.Errorf("%s: writing a file got %v, want %v", name, got, want.workspace)
		}

		if got := refuse(".git/config"); !errors.Is(got, want.history) {
			t.Errorf("%s: writing the metadata got %v, want %v", name, got, want.history)
		}
	}
}

func TestTheModeIsSafeToSwapWhileItIsBeingRead(t *testing.T) {
	self := NewMode(writable())

	var waiting sync.WaitGroup

	for range 100 {
		waiting.Add(3)

		go func() {
			defer waiting.Done()

			self.Toggle(capWrite)
		}()

		go func() {
			defer waiting.Done()

			self.Toggle(capGit)
		}()

		go func() {
			defer waiting.Done()

			_ = self.Current()
		}()
	}

	waiting.Wait()

	if currentCaps := self.Current(); !currentCaps.has(capWrite) || currentCaps.has(capGit) {
		t.Error("expected an even number of swaps to leave the mode where it started")
	}
}
