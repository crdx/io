package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/output"
	"crdx.org/io/cmd/oh/store"
)

type faultingConversationLog struct {
	SessionLogger

	failedKind         agent.Kind
	itemFailure        error
	completionFailure  error
	warnings           []error
	eventAttempts      int
	failedAttempts     int
	completionAttempts int
}

func (self *faultingConversationLog) Event(event agent.Event) error {
	self.eventAttempts++
	if event.Kind == self.failedKind {
		self.failedAttempts++
		return errors.New("canonical write failed")
	}
	return self.SessionLogger.Event(event)
}

func (self *faultingConversationLog) Item(item json.RawMessage) error {
	if self.itemFailure != nil {
		return self.itemFailure
	}
	return self.SessionLogger.Item(item)
}

func (self *faultingConversationLog) CompleteTurn() error {
	self.completionAttempts++
	if self.completionFailure != nil {
		return self.completionFailure
	}
	return self.SessionLogger.CompleteTurn()
}

func (self *faultingConversationLog) TakeWarnings() []error {
	warnings := self.warnings
	self.warnings = nil
	return warnings
}

func newStorageFaultHarness(log SessionLogger, assistant *agent.Agent) *Harness {
	testHarness := &Harness{
		agent:    assistant,
		screen:   output.New(&bytes.Buffer{}),
		recorder: recordSession(log),
	}
	testHarness.currentTurn = Turn{
		painter: testHarness.newPainter(false),
		Stream:  testTimedTurnStream(false, time.Now(), time.Time{}),
	}
	return testHarness
}

func TestCanonicalEventWriteFailuresWarnWithoutRecursing(t *testing.T) {
	for _, failedKind := range []agent.Kind{
		agent.UserMessageEvent,
		agent.ModelReasoningEvent,
		agent.ModelMessageEvent,
		agent.FailureEvent,
	} {
		t.Run(string(failedKind), func(t *testing.T) {
			directory := t.TempDir()
			innerLog, err := store.Create(directory, store.Meta{})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = innerLog.Close() }()

			log := &faultingConversationLog{SessionLogger: innerLog, failedKind: failedKind}
			testHarness := newStorageFaultHarness(log, agent.New("", quietProvider{}, nil))
			if failedKind != agent.UserMessageEvent {
				testHarness.recordEvent(agent.Event{Kind: agent.UserMessageEvent, Text: "start"})
			}
			testHarness.recordEvent(agent.Event{Kind: failedKind, Text: "failed event"})

			if log.failedAttempts != 1 {
				t.Errorf("failed %d writes, want 1", log.failedAttempts)
			}
			if log.eventAttempts > 3 {
				t.Errorf("made %d event write attempts", log.eventAttempts)
			}
			if failedKind != agent.UserMessageEvent {
				storedSession, err := store.Read(directory, innerLog.Name())
				if err != nil {
					t.Fatal(err)
				}
				if len(storedSession.Events) == 0 || storedSession.Events[0].Kind != agent.UserMessageEvent {
					t.Errorf("canonical journal lost its successful prefix: %+v", storedSession.Events)
				}
			}
		})
	}
}

func TestProviderItemWriteFailureWarnsAndLeavesCanonicalJournalReadable(t *testing.T) {
	directory := t.TempDir()
	innerLog, err := store.Create(directory, store.Meta{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = innerLog.Close() }()
	if err := innerLog.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "start"}); err != nil {
		t.Fatal(err)
	}

	provider := &fakeProvider{items: []json.RawMessage{json.RawMessage(`{"type":"reasoning"}`)}}
	log := &faultingConversationLog{
		SessionLogger: innerLog,
		itemFailure:   errors.New("item write failed"),
	}
	testHarness := newStorageFaultHarness(log, agent.New("", provider, nil))
	testHarness.finish()

	storedSession, err := store.Read(directory, innerLog.Name())
	if err != nil {
		t.Fatal(err)
	}
	if len(storedSession.Items) != 0 {
		t.Errorf("stored failed provider items: %s", storedSession.Items)
	}
	if storedSession.TurnCompletions != 0 {
		t.Errorf("completed %d turns after the provider state failed", storedSession.TurnCompletions)
	}
	if log.completionAttempts != 0 {
		t.Errorf("attempted %d completion writes after the provider state failed", log.completionAttempts)
	}
	if len(storedSession.Events) != 2 ||
		storedSession.Events[1].Kind != agent.HarnessMessageEvent ||
		storedSession.Events[1].Status != agent.ErrorStatus ||
		!strings.Contains(storedSession.Events[1].Text, "item write failed") {
		t.Errorf("unexpected canonical events: %+v", storedSession.Events)
	}
}

func TestTurnCompletionIsStoredOnlyAfterProviderStateSucceeds(t *testing.T) {
	for name, completionFailure := range map[string]error{
		"success":                  nil,
		"completion write failure": errors.New("completion write failed"),
	} {
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			innerLog, err := store.Create(directory, store.Meta{})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = innerLog.Close() }()
			if err := innerLog.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "start"}); err != nil {
				t.Fatal(err)
			}

			provider := &fakeProvider{items: []json.RawMessage{json.RawMessage(`{"type":"message"}`)}}
			log := &faultingConversationLog{SessionLogger: innerLog, completionFailure: completionFailure}
			testHarness := newStorageFaultHarness(log, agent.New("", provider, nil))
			testHarness.finish()

			storedSession, err := store.Read(directory, innerLog.Name())
			if err != nil {
				t.Fatal(err)
			}
			if len(storedSession.Items) != 1 {
				t.Fatalf("stored %d provider items, want 1", len(storedSession.Items))
			}
			wantTurnCompletions := 1
			if completionFailure != nil {
				wantTurnCompletions = 0
			}
			if storedSession.TurnCompletions != wantTurnCompletions {
				t.Errorf("completed %d turns, want %d", storedSession.TurnCompletions, wantTurnCompletions)
			}
			if log.completionAttempts != 1 {
				t.Errorf("attempted %d completion writes, want 1", log.completionAttempts)
			}
		})
	}
}

func TestAuxiliaryRecorderWarningsAreShownOnceAndRemainCanonical(t *testing.T) {
	directory := t.TempDir()
	innerLog, err := store.Create(directory, store.Meta{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = innerLog.Close() }()
	if err := innerLog.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "start"}); err != nil {
		t.Fatal(err)
	}

	log := &faultingConversationLog{
		SessionLogger: innerLog,
		warnings: []error{
			errors.New("chat.md recording disabled: transcript append failed"),
			errors.New("wire.http recording disabled: wire append failed"),
		},
	}
	testHarness := newStorageFaultHarness(log, agent.New("", quietProvider{}, nil))
	testHarness.showStorageWarnings()
	testHarness.showStorageWarnings()

	storedSession, err := store.Read(directory, innerLog.Name())
	if err != nil {
		t.Fatal(err)
	}
	var warningEvents int
	for _, event := range storedSession.Events {
		if event.Kind == agent.HarnessMessageEvent {
			warningEvents++
		}
	}
	if warningEvents != 2 {
		t.Errorf("stored %d warning events, want 2", warningEvents)
	}
}
