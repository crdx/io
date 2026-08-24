package store

import (
	"os"
	"path/filepath"
	"testing"

	"crdx.org/io/agent"
)

func TestReadRejectsASessionWithoutAHead(t *testing.T) {
	directory := t.TempDir()
	if err := os.Mkdir(filepath.Join(directory, "broken"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "broken", "session.jsonl"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(directory, "broken"); err == nil {
		t.Error("expected an empty session to be rejected")
	}
}

func TestSessionIDsCannotEscapeTheSessionDirectory(t *testing.T) {
	if _, err := Read(t.TempDir(), "../somewhere-else"); err == nil {
		t.Error("expected a path-like session ID to be rejected")
	}
}

func TestASessionCanResumeOnlyAfterEveryTurnCompletionWasStored(t *testing.T) {
	for name, test := range map[string]struct {
		events          []agent.Event
		turnCompletions int
		canResume       bool
	}{
		"empty": {canResume: true},
		"startup only": {
			events:    []agent.Event{{Kind: agent.StartupEvent}},
			canResume: true,
		},
		"user message": {
			events: []agent.Event{{Kind: agent.UserMessageEvent}},
		},
		"visible answer without completion": {
			events: []agent.Event{{Kind: agent.UserMessageEvent}, {Kind: agent.ModelMessageEvent}},
		},
		"failure without completion": {
			events: []agent.Event{{Kind: agent.UserMessageEvent}, {Kind: agent.FailureEvent}},
		},
		"interruption without completion": {
			events: []agent.Event{{Kind: agent.UserMessageEvent}, {Kind: agent.InterruptionEvent}},
		},
		"completed turn": {
			events:          []agent.Event{{Kind: agent.UserMessageEvent}, {Kind: agent.ModelMessageEvent}},
			turnCompletions: 1,
			canResume:       true,
		},
		"unfinished first turn followed by completed second turn": {
			events:          []agent.Event{{Kind: agent.UserMessageEvent}, {Kind: agent.UserMessageEvent}, {Kind: agent.ModelMessageEvent}},
			turnCompletions: 1,
		},
		"unfinished second turn": {
			events:          []agent.Event{{Kind: agent.UserMessageEvent}, {Kind: agent.ModelMessageEvent}, {Kind: agent.UserMessageEvent}},
			turnCompletions: 1,
		},
		"impossible extra completion": {
			turnCompletions: 1,
		},
	} {
		t.Run(name, func(t *testing.T) {
			storedSession := &Session{Events: test.events, TurnCompletions: test.turnCompletions}
			if got := storedSession.CanResume(); got != test.canResume {
				t.Errorf("CanResume() = %v, want %v", got, test.canResume)
			}
		})
	}
}
