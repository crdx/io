package main

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/key"
	"crdx.org/io/cmd/oh/line"
	"crdx.org/io/cmd/oh/output"
	"crdx.org/io/cmd/oh/store"
	"crdx.org/io/cmd/oh/style"
)

func TestEscapeAtRestDoesNotPanic(t *testing.T) {
	self := &conversation{screen: output.New(&bytes.Buffer{})}
	input := line.NewInput(nil)

	defer func() {
		if panicValue := recover(); panicValue != nil {
			t.Fatalf("escape at rest panicked: %v", panicValue)
		}
	}()

	if !self.apply(input, nil, key.Key{Code: key.Escape}) {
		t.Error("expected escape at rest to leave the conversation open")
	}
}

func TestTerminalFocusEventsAreTracked(t *testing.T) {
	self := &conversation{terminalFocused: true}
	input := line.NewInput(nil)

	self.apply(input, nil, key.Key{Code: key.FocusOut})
	if self.terminalFocused {
		t.Error("expected focus-out to mark the terminal unfocused")
	}

	self.apply(input, nil, key.Key{Code: key.FocusIn})
	if !self.terminalFocused {
		t.Error("expected focus-in to mark the terminal focused")
	}
}

func TestControlDStopsATurnBeforeItIsAWayOut(t *testing.T) {
	self := &conversation{screen: output.New(&bytes.Buffer{})}
	input := line.NewInput(nil)

	keypress := key.Key{Code: key.Rune, Value: 'd', Mod: key.Ctrl}

	stopped := false

	self.turn = turn{isRunning: true, stop: func() { stopped = true }}

	if !self.apply(input, nil, keypress) {
		t.Error("expected ctrl+d during a turn to stop the turn rather than the harness")
	}

	if !stopped || !self.turn.isCancelled {
		t.Error("expected the turn to have been cancelled, as escape cancels it")
	}

	self.turn = turn{}

	if self.apply(input, nil, keypress) {
		t.Error("expected ctrl+d at rest to be the way out")
	}

	input.Apply(key.Key{Code: key.Rune, Value: 'a'}, false)

	if !self.apply(input, nil, keypress) {
		t.Error("expected ctrl+d on a line with something on it to leave the harness running")
	}
}

func TestTwoReturnsOnAnEmptyIdleLineSendTheGetOnWithItMessage(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	self.getOnWithItMessage = "carry on"
	history := line.NewHistory("", historyLimit)
	input := line.NewInput(history)

	self.apply(input, history, key.Key{Code: key.Enter})
	if self.turn.isRunning {
		t.Error("expected the first return to do nothing")
	}

	self.apply(input, history, key.Key{Code: key.Enter})
	if !self.turn.isRunning {
		t.Fatal("expected the second return to start a turn")
	}

	for report := range self.turn.events {
		self.take(report)
	}
	self.finish()

	for _, record := range self.transcript {
		if record.event.Kind == agent.Prompt && record.event.Text == "carry on" {
			return
		}
	}

	t.Error("expected the configured prompt")
}

func TestAcceptedInputCanImmediatelyBeRecalled(t *testing.T) {
	self := &conversation{turn: turn{isRunning: true, stop: func() {}}}
	history := line.NewHistory("", historyLimit)
	input := line.NewInput(history)

	for _, value := range "latest" {
		self.apply(input, history, key.Key{Code: key.Rune, Value: value})
	}
	self.apply(input, history, key.Key{Code: key.Enter})
	self.apply(input, history, key.Key{Code: key.Up})

	if input.Text() != "latest" {
		t.Errorf("expected the latest input to be recalled, got %q", input.Text())
	}
}

func TestChangingCapabilitiesRestartsTheTurnWithTheChangeAsItsPrompt(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	history := line.NewHistory("", historyLimit)
	input := line.NewInput(history)

	self.start("first")
	interruptedEvents := self.turn.events

	self.apply(input, history, key.Key{Code: key.Rune, Value: 'x', Mod: key.Ctrl})
	self.apply(input, history, key.Key{Code: key.Rune, Value: 'w'})

	if !self.turn.isCancelled {
		t.Fatal("expected the capability change to interrupt the turn")
	}

	for report := range interruptedEvents {
		self.take(report)
	}
	self.finish()

	if !self.turn.isRunning {
		t.Fatal("expected the capability change to start a replacement turn")
	}

	for report := range self.turn.events {
		self.take(report)
	}
	self.finish()

	var prompts []string
	for _, record := range self.transcript {
		if record.event.Kind == agent.Prompt {
			prompts = append(prompts, record.event.Text)
		}
	}

	wantPrompts := []string{"first", nowReadOnly}
	if !slices.Equal(prompts, wantPrompts) {
		t.Errorf("got prompts %q, want %q", prompts, wantPrompts)
	}
}

func TestTwoReturnsOnAnEmptyLineReplaceTheRunningTurnWithAGetOnWithItMessage(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	history := line.NewHistory("", historyLimit)
	input := line.NewInput(history)

	self.start("first")
	interruptedEvents := self.turn.events

	self.apply(input, history, key.Key{Code: key.Enter})
	if self.turn.isCancelled {
		t.Error("expected the first return to leave the turn running")
	}

	self.apply(input, history, key.Key{Code: key.Enter})
	if !self.turn.isCancelled {
		t.Error("expected the second return to cancel the turn")
	}

	for report := range interruptedEvents {
		self.take(report)
	}
	self.finish()

	if !self.turn.isRunning {
		t.Fatal("expected a continuation to start after the interrupted turn")
	}

	for report := range self.turn.events {
		self.take(report)
	}
	self.finish()

	var prompts []string
	var interrupted bool
	for _, record := range self.transcript {
		if record.event.Kind == agent.Prompt {
			prompts = append(prompts, record.event.Text)
		}
		interrupted = interrupted || record.event.Kind == agent.Interrupted
	}

	wantPrompts := []string{"first", defaultGetOnWithItMessage}
	if !slices.Equal(prompts, wantPrompts) {
		t.Errorf("got prompts %q, want %q", prompts, wantPrompts)
	}
	if !interrupted {
		t.Error("expected the replacement to record an interruption")
	}
}

type eventsAfterCancellationProvider struct {
	quietProvider
}

func (eventsAfterCancellationProvider) Send(ctx context.Context, yield agent.Yield) (agent.Reply, error) {
	for _, call := range []agent.Event{
		{Kind: agent.Call, ID: "a", Name: "first", Subject: "first"},
		{Kind: agent.Call, ID: "b", Name: "second", Subject: "second"},
	} {
		if !yield(call) {
			return agent.Reply{}, nil
		}
	}

	<-ctx.Done()

	for _, result := range []agent.Event{
		{Kind: agent.Result, ID: "a", Name: "first"},
		{Kind: agent.Result, ID: "b", Name: "second"},
	} {
		if !yield(result) {
			return agent.Reply{}, nil
		}
	}

	return agent.Reply{}, ctx.Err()
}

func TestCompletedEventsAreRenderedAfterCancellation(t *testing.T) {
	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = log.Close() }()

	self := &conversation{
		assistant: agent.New("", eventsAfterCancellationProvider{}, nil),
		screen:    output.New(&bytes.Buffer{}),
		log:       log,
		mode:      NewMode(capRead | capWrite),
	}

	self.start("first")
	events := self.turn.events

	calls := 0
	for calls < 2 {
		report := <-events
		self.take(report)
		if report.event.Kind == agent.Call {
			calls++
		}
	}

	self.interrupt()

	for report := range events {
		self.take(report)
	}
	self.finish()

	results := 0
	for _, record := range self.transcript {
		if record.event.Kind == agent.Result {
			results++
		}
	}

	if results != 2 {
		t.Errorf("expected both completed results to be rendered, got %d", results)
	}
}

func TestAcceptedReplacementDisappearsWhileCancelledTurnStillRuns(t *testing.T) {
	self := &conversation{
		turn: turn{isRunning: true, events: make(chan turnEvent), stop: func() {}},
	}
	history := line.NewHistory("", historyLimit)
	input := line.NewInput(history)

	for _, value := range "dfd" {
		self.apply(input, history, key.Key{Code: key.Rune, Value: value})
	}
	self.apply(input, history, key.Key{Code: key.Enter})

	if input.Text() != "" {
		t.Fatalf("expected accepted editor input to disappear, got %q", input.Text())
	}
	if !self.turn.isRunning {
		t.Fatal("expected cancelled turn to remain running until its event channel closes")
	}
	if !self.turn.isCancelled {
		t.Fatal("expected the active turn to be marked cancelled")
	}
	if !self.queuedTurn || self.queuedPrompt != "dfd" {
		t.Fatalf("expected dfd to exist only as an invisible queued prompt, got queued=%t prompt=%q", self.queuedTurn, self.queuedPrompt)
	}
}

func TestReturnSendsInputAfterTheInterruptedTurnFinishes(t *testing.T) {
	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var screenOutput bytes.Buffer
	self := &conversation{
		assistant: agent.New("", quietProvider{}, nil),
		screen:    output.New(&screenOutput),
		log:       log,
		mode:      NewMode(capRead | capWrite),
	}
	history := line.NewHistory("", historyLimit)
	input := line.NewInput(history)

	self.start("first")
	interruptedEvents := self.turn.events

	for _, value := range "follow up" {
		self.apply(input, history, key.Key{Code: key.Rune, Value: value})
	}
	self.apply(input, history, key.Key{Code: key.Enter})

	for report := range interruptedEvents {
		self.take(report)
	}
	self.finish()

	if !self.turn.isRunning {
		t.Fatal("expected the accepted input to start another turn")
	}

	for report := range self.turn.events {
		self.take(report)
	}
	self.finish()

	if strings.Contains(style.Plain(screenOutput.String()), "Interrupted") {
		t.Errorf("expected the replaced turn to end silently, got %q", style.Plain(screenOutput.String()))
	}

	if err := log.Close(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	storedSession, err := store.Read(directory, log.ID())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var sent, interruptionStored bool
	for _, event := range storedSession.Events {
		sent = sent || event.Kind == agent.Prompt && event.Text == "follow up"
		interruptionStored = interruptionStored || event.Kind == agent.Interrupted
	}

	if !sent {
		t.Error("expected the accepted input to be sent after the interruption")
	}
	if !interruptionStored {
		t.Error("expected the interruption to be stored in the session log")
	}
}

func TestAStoppedTurnIsStoredAsAnInterruption(t *testing.T) {
	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	self := &conversation{
		assistant: agent.New("", quietProvider{}, nil),
		screen:    output.New(&bytes.Buffer{}),
		log:       log,
		mode:      NewMode(capRead | capWrite),
	}

	self.start("first")
	self.interrupt()

	for report := range self.turn.events {
		self.take(report)
	}
	self.finish()

	if err := log.Close(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	storedSession, err := store.Read(directory, log.ID())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, event := range storedSession.Events {
		if event.Kind == agent.Interrupted {
			return
		}
	}

	t.Error("expected the interruption to be stored in the session log")
}

func TestEscapeTakesBackAQueuedReplacementWithoutAnnouncingTheInterruption(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	history := line.NewHistory("", historyLimit)
	input := line.NewInput(history)

	self.start("first")
	interruptedEvents := self.turn.events

	for _, value := range "follow up" {
		self.apply(input, history, key.Key{Code: key.Rune, Value: value})
	}
	self.apply(input, history, key.Key{Code: key.Enter})
	self.apply(input, history, key.Key{Code: key.Escape})

	for report := range interruptedEvents {
		self.take(report)
	}
	self.finish()

	if self.turn.isRunning {
		t.Error("expected the taken-back replacement to leave no turn running")
	}

	for _, record := range self.transcript {
		if record.event.Kind == agent.Prompt && record.event.Text == "follow up" {
			t.Error("expected the taken-back replacement not to be sent")
		}
	}

	if strings.Contains(style.Plain(screenOutput.String()), "Interrupted") {
		t.Errorf("expected the interruption to stay out of the scrollback, got %q", style.Plain(screenOutput.String()))
	}
}

func TestControlDStopsATurnWhateverHasBeenTyped(t *testing.T) {
	self := &conversation{screen: output.New(&bytes.Buffer{})}
	input := line.NewInput(nil)

	input.Apply(key.Key{Code: key.Rune, Value: 'a'}, false)

	stopped := false

	self.turn = turn{isRunning: true, stop: func() { stopped = true }}

	if !self.apply(input, nil, key.Key{Code: key.Rune, Value: 'd', Mod: key.Ctrl}) {
		t.Error("expected ctrl+d during a turn to stop the turn rather than the harness")
	}

	if !stopped || !self.turn.isCancelled {
		t.Error("expected the turn to have been cancelled")
	}

	if input.Text() != "a" {
		t.Errorf("expected what was typed to be left alone, got %q", input.Text())
	}
}
