package main

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"crdx.org/io/internal/file"
)

const (
	nowReadOnly      = "The workspace is now read-only."
	nowReadWrite     = "The workspace is now read-write."
	gitReadOnly      = "The .git directory is now read-only."
	gitWritable      = "The .git directory is now read-write."
	backgroundOn     = "Background processes can now outlive shell commands."
	backgroundKilled = "Background processes have been killed and will no longer outlive shell commands."
	shellGranted     = "The exec tool can now run shell commands."
	shellWithheld    = "The exec tool is now refused, and will turn away every command until it is granted again."
)

func writable() caps { return capRead | capWrite }

// The prompt of a fresh conversation states the mode it opened in, so there is nothing to add.
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

// The history is its own bit, and moving it says so without saying anything about the workspace.
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

// Both bits moving between one turn and the next is one note with a clause apiece.
func TestBothSwapsBetweenTurnsAreAnnouncedTogether(t *testing.T) {
	self := NewMode(writable())

	self.Toggle(capWrite)
	self.Toggle(capGit)

	if got := self.Inject(); got != nowReadOnly+" "+gitWritable {
		t.Errorf("expected both clauses, got %q", got)
	}
}

// A mode swapped and swapped straight back is the mode the model already knows about.
func TestAModeSwappedTwiceIsNotAnnounced(t *testing.T) {
	self := NewMode(writable())

	self.Toggle(capWrite)
	self.Toggle(capWrite)

	if got := self.Inject(); got != "" {
		t.Errorf("expected nothing to be announced, got %q", got)
	}
}

// A resumed conversation replays the prompt it was stored with, which states what that conversation
// opened with rather than what this run was started with.
func TestAResumedConversationAlwaysSaysWhatTheModeAllows(t *testing.T) {
	got := NewResumedMode(writable()).Inject()

	for _, clause := range []string{nowReadWrite, gitReadOnly, backgroundKilled, shellWithheld} {
		if !strings.Contains(got, clause) {
			t.Errorf("expected %q in %q", clause, got)
		}
	}
}

// The shell is switched like anything else, the tool being offered whether or not it may run, so
// what changes is what it answers rather than whether the model has heard of it.
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

// The four states are what the rule composes, and each of them means something. The last is the
// commit-only one, where work already done may be stored without a line of it being edited.
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
		refuse := refusal(NewMode(want.currentCaps))

		if got := refuse("main.go"); !errors.Is(got, want.workspace) {
			t.Errorf("%s: writing a file got %v, want %v", name, got, want.workspace)
		}

		if got := refuse(".git/config"); !errors.Is(got, want.history) {
			t.Errorf("%s: writing the metadata got %v, want %v", name, got, want.history)
		}
	}
}

// A keypress swaps the mode on one goroutine while a turn reads it on another.
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
