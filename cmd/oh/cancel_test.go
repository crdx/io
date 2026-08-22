package main

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/edit"
	"crdx.org/io/cmd/oh/key"
	"crdx.org/io/cmd/oh/output"
	"crdx.org/io/cmd/oh/store"
	"crdx.org/io/cmd/oh/style"
)

func TestEscapeAtRestDoesNotPanic(t *testing.T) {
	self := &Harness{screen: output.New(&bytes.Buffer{})}
	editor := edit.NewInput(nil)

	defer func() {
		if panicValue := recover(); panicValue != nil {
			t.Fatalf("escape at rest panicked: %v", panicValue)
		}
	}()

	if !self.apply(editor, nil, key.Key{Code: key.Escape}) {
		t.Error("expected escape at rest to leave the conversation open")
	}
}

func TestTerminalFocusEventsAreTracked(t *testing.T) {
	self := &Harness{terminalFocused: true}
	editor := edit.NewInput(nil)

	self.apply(editor, nil, key.Key{Code: key.FocusOut})
	if self.terminalFocused {
		t.Error("expected focus-out to mark the terminal unfocused")
	}

	self.apply(editor, nil, key.Key{Code: key.FocusIn})
	if !self.terminalFocused {
		t.Error("expected focus-in to mark the terminal focused")
	}
}

func TestControlDStopsATurnBeforeItIsAWayOut(t *testing.T) {
	self := &Harness{screen: output.New(&bytes.Buffer{})}
	editor := edit.NewInput(nil)

	keypress := key.Key{Code: key.Rune, Value: 'd', Mod: key.Ctrl}

	stopped := false

	self.turn = Turn{isRunning: true, cancel: func() { stopped = true }}

	if !self.apply(editor, nil, keypress) {
		t.Error("expected ctrl+d during a turn to stop the turn rather than the harness")
	}

	if !stopped || !self.turn.isCancelled {
		t.Error("expected the turn to have been cancelled, as escape cancels it")
	}

	self.turn = Turn{}

	if self.apply(editor, nil, keypress) {
		t.Error("expected ctrl+d at rest to be the way out")
	}

	editor.Apply(key.Key{Code: key.Rune, Value: 'a'}, false)

	if !self.apply(editor, nil, keypress) {
		t.Error("expected ctrl+d on a line with something on it to leave the harness running")
	}
}

func TestTwoReturnsOnAnEmptyIdleLineSendTheGetOnWithItMessage(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	self.getOnWithItMessage = "carry on"
	history := edit.NewHistory("", historyLimit)
	editor := edit.NewInput(history)

	self.apply(editor, history, key.Key{Code: key.Enter})
	if self.turn.isRunning {
		t.Error("expected the first return to do nothing")
	}

	self.apply(editor, history, key.Key{Code: key.Enter})
	if !self.turn.isRunning {
		t.Fatal("expected the second return to start a turn")
	}

	for report := range self.turn.events {
		self.takeTurn(report)
	}
	self.finish()

	for _, record := range self.events {
		if record.Kind == agent.UserMessage && record.Text == "carry on" {
			return
		}
	}

	t.Error("expected the configured prompt")
}

func TestAcceptedInputCanImmediatelyBeRecalled(t *testing.T) {
	self := &Harness{turn: Turn{isRunning: true, cancel: func() {}}}
	history := edit.NewHistory("", historyLimit)
	editor := edit.NewInput(history)

	for _, value := range "latest" {
		self.apply(editor, history, key.Key{Code: key.Rune, Value: value})
	}
	self.apply(editor, history, key.Key{Code: key.Enter})
	self.apply(editor, history, key.Key{Code: key.Up})

	if editor.Text() != "latest" {
		t.Errorf("expected the latest input to be recalled, got %q", editor.Text())
	}
}

func TestChangingCapabilitiesRestartsTheTurnWithTheChangeAsItsPrompt(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	history := edit.NewHistory("", historyLimit)
	editor := edit.NewInput(history)

	self.start("first")
	interruptedEvents := self.turn.events

	self.apply(editor, history, key.Key{Code: key.Rune, Value: 'x', Mod: key.Ctrl})
	self.apply(editor, history, key.Key{Code: key.Rune, Value: 'w'})

	if !self.turn.isCancelled {
		t.Fatal("expected the capability change to interrupt the turn")
	}

	for report := range interruptedEvents {
		self.takeTurn(report)
	}
	self.finish()

	if !self.turn.isRunning {
		t.Fatal("expected the capability change to start a replacement turn")
	}

	for report := range self.turn.events {
		self.takeTurn(report)
	}
	self.finish()

	var messages []string
	for _, record := range self.events {
		if record.Kind == agent.UserMessage {
			messages = append(messages, record.Text)
		}
	}

	wantMessages := []string{"first", workspaceNowReadOnly()}
	if !slices.Equal(messages, wantMessages) {
		t.Errorf("got messages %q, want %q", messages, wantMessages)
	}
}

func workspaceNowReadOnly() string {
	withdrawn := caps.NewMode(caps.Read | caps.Write)
	withdrawn.Toggle(caps.Write)

	return withdrawn.Inject()
}

func TestTwoReturnsOnAnEmptyLineReplaceTheRunningTurnWithAGetOnWithItMessage(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	history := edit.NewHistory("", historyLimit)
	editor := edit.NewInput(history)

	self.start("first")
	interruptedEvents := self.turn.events

	self.apply(editor, history, key.Key{Code: key.Enter})
	if self.turn.isCancelled {
		t.Error("expected the first return to leave the turn running")
	}

	self.apply(editor, history, key.Key{Code: key.Enter})
	if !self.turn.isCancelled {
		t.Error("expected the second return to cancel the turn")
	}

	for report := range interruptedEvents {
		self.takeTurn(report)
	}
	self.finish()

	if !self.turn.isRunning {
		t.Fatal("expected a continuation to start after the interrupted turn")
	}

	for report := range self.turn.events {
		self.takeTurn(report)
	}
	self.finish()

	var messages []string
	var interrupted bool
	for _, record := range self.events {
		if record.Kind == agent.UserMessage {
			messages = append(messages, record.Text)
		}
		interrupted = interrupted || record.Kind == agent.Interruption
	}

	wantMessages := []string{"first", defaultGetOnWithItMessage}
	if !slices.Equal(messages, wantMessages) {
		t.Errorf("got messages %q, want %q", messages, wantMessages)
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
		{Kind: agent.ToolCallRequest, ID: "a", Name: "first", Rendering: agent.Rendering{Subject: "first"}},
		{Kind: agent.ToolCallRequest, ID: "b", Name: "second", Rendering: agent.Rendering{Subject: "second"}},
	} {
		if !yield(call) {
			return agent.Reply{}, nil
		}
	}

	<-ctx.Done()

	for _, result := range []agent.Event{
		{Kind: agent.ToolCallResult, ID: "a", Name: "first"},
		{Kind: agent.ToolCallResult, ID: "b", Name: "second"},
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

	self := &Harness{
		agent:  agent.New("", eventsAfterCancellationProvider{}, nil),
		screen: output.New(&bytes.Buffer{}),
		log:    log,
		mode:   caps.NewMode(caps.Read | caps.Write),
	}

	self.start("first")
	events := self.turn.events

	calls := 0
	for calls < 2 {
		report := <-events
		self.takeTurn(report)
		if report.event.Kind == agent.ToolCallRequest {
			calls++
		}
	}

	self.interruptTurn()

	for report := range events {
		self.takeTurn(report)
	}
	self.finish()

	results := 0
	for _, record := range self.events {
		if record.Kind == agent.ToolCallResult {
			results++
		}
	}

	if results != 2 {
		t.Errorf("expected both completed results to be rendered, got %d", results)
	}
}

func TestAcceptedReplacementDisappearsWhileCancelledTurnStillRuns(t *testing.T) {
	self := &Harness{
		turn: Turn{isRunning: true, events: make(chan TurnEvent), cancel: func() {}},
	}
	history := edit.NewHistory("", historyLimit)
	editor := edit.NewInput(history)

	for _, value := range "dfd" {
		self.apply(editor, history, key.Key{Code: key.Rune, Value: value})
	}
	self.apply(editor, history, key.Key{Code: key.Enter})

	if editor.Text() != "" {
		t.Fatalf("expected accepted editor input to disappear, got %q", editor.Text())
	}
	if !self.turn.isRunning {
		t.Fatal("expected cancelled turn to remain running until its event channel closes")
	}
	if !self.turn.isCancelled {
		t.Fatal("expected the active turn to be marked cancelled")
	}
	if !self.queuedTurn.isReplacement || self.queuedTurn.nextMessage != "dfd" {
		t.Fatalf("expected dfd to exist only as an invisible queued prompt, got queued=%t prompt=%q", self.queuedTurn.isReplacement, self.queuedTurn.nextMessage)
	}
}

func TestReturnSendsInputAfterTheInterruptedTurnFinishes(t *testing.T) {
	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var screenOutput bytes.Buffer
	self := &Harness{
		agent:  agent.New("", quietProvider{}, nil),
		screen: output.New(&screenOutput),
		log:    log,
		mode:   caps.NewMode(caps.Read | caps.Write),
	}
	history := edit.NewHistory("", historyLimit)
	editor := edit.NewInput(history)

	self.start("first")
	interruptedEvents := self.turn.events

	for _, value := range "follow up" {
		self.apply(editor, history, key.Key{Code: key.Rune, Value: value})
	}
	self.apply(editor, history, key.Key{Code: key.Enter})

	for report := range interruptedEvents {
		self.takeTurn(report)
	}
	self.finish()

	if !self.turn.isRunning {
		t.Fatal("expected the accepted input to start another turn")
	}

	for report := range self.turn.events {
		self.takeTurn(report)
	}
	self.finish()

	if strings.Contains(style.Plain(screenOutput.String()), "Interrupted") {
		t.Errorf("expected the replaced turn to end silently, got %q", style.Plain(screenOutput.String()))
	}

	if err := log.Close(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	storedSession, err := store.Read(directory, log.Name())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var sent, interruptionStored bool
	for _, event := range storedSession.Events {
		sent = sent || event.Kind == agent.UserMessage && event.Text == "follow up"
		interruptionStored = interruptionStored || event.Kind == agent.Interruption
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

	self := &Harness{
		agent:  agent.New("", quietProvider{}, nil),
		screen: output.New(&bytes.Buffer{}),
		log:    log,
		mode:   caps.NewMode(caps.Read | caps.Write),
	}

	self.start("first")
	self.interruptTurn()

	for report := range self.turn.events {
		self.takeTurn(report)
	}
	self.finish()

	if err := log.Close(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	storedSession, err := store.Read(directory, log.Name())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, event := range storedSession.Events {
		if event.Kind == agent.Interruption {
			return
		}
	}

	t.Error("expected the interruption to be stored in the session log")
}

func TestEscapeTakesBackAQueuedReplacementWithoutAnnouncingTheInterruption(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	history := edit.NewHistory("", historyLimit)
	editor := edit.NewInput(history)

	self.start("first")
	interruptedEvents := self.turn.events

	for _, value := range "follow up" {
		self.apply(editor, history, key.Key{Code: key.Rune, Value: value})
	}
	self.apply(editor, history, key.Key{Code: key.Enter})
	self.apply(editor, history, key.Key{Code: key.Escape})

	for report := range interruptedEvents {
		self.takeTurn(report)
	}
	self.finish()

	if self.turn.isRunning {
		t.Error("expected the taken-back replacement to leave no turn running")
	}

	for _, record := range self.events {
		if record.Kind == agent.UserMessage && record.Text == "follow up" {
			t.Error("expected the taken-back replacement not to be sent")
		}
	}

	if strings.Contains(style.Plain(screenOutput.String()), "Interrupted") {
		t.Errorf("expected the interruption to stay out of the scrollback, got %q", style.Plain(screenOutput.String()))
	}
}

func TestControlDStopsATurnWhateverHasBeenTyped(t *testing.T) {
	self := &Harness{screen: output.New(&bytes.Buffer{})}
	editor := edit.NewInput(nil)

	editor.Apply(key.Key{Code: key.Rune, Value: 'a'}, false)

	stopped := false

	self.turn = Turn{isRunning: true, cancel: func() { stopped = true }}

	if !self.apply(editor, nil, key.Key{Code: key.Rune, Value: 'd', Mod: key.Ctrl}) {
		t.Error("expected ctrl+d during a turn to stop the turn rather than the harness")
	}

	if !stopped || !self.turn.isCancelled {
		t.Error("expected the turn to have been cancelled")
	}

	if editor.Text() != "a" {
		t.Errorf("expected what was typed to be left alone, got %q", editor.Text())
	}
}

func TestCancellingTwiceDropsTheQueueAndCancellingOnceKeepsIt(t *testing.T) {
	tests := map[string]struct {
		isCancelled bool
		wantKept    bool
	}{
		"a turn already cancelled": {isCancelled: true, wantKept: false},
		"a turn still running":     {isCancelled: false, wantKept: true},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			self := &Harness{
				turn: Turn{isRunning: true, isCancelled: test.isCancelled, cancel: func() {}},
			}
			self.queuedTurn.nextMessage = "later"
			self.queuedTurn.isReplacement = true
			self.queuedTurn.isModeChange = true

			self.cancelTurn()

			kept := self.queuedTurn.isReplacement && self.queuedTurn.isModeChange && self.queuedTurn.nextMessage == "later"
			if kept != test.wantKept {
				t.Errorf(
					"expected the queue kept=%t, got queued=%t mode=%t prompt=%q",
					test.wantKept, self.queuedTurn.isReplacement, self.queuedTurn.isModeChange, self.queuedTurn.nextMessage,
				)
			}
		})
	}
}

func TestAQueuedPromptStartsAndTakesTheQueuedModeChangeWithIt(t *testing.T) {
	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = log.Close() }()

	self := &Harness{
		agent:  agent.New("", quietProvider{}, nil),
		screen: output.New(&bytes.Buffer{}),
		log:    log,
		mode:   caps.NewMode(caps.Read | caps.Write),
	}

	self.start("first")
	self.toggleCapability(caps.Write)
	self.replaceTurn("second")

	if !self.queuedTurn.isReplacement || !self.queuedTurn.isModeChange || self.queuedTurn.nextMessage != "second" {
		t.Fatalf(
			"expected both a queued prompt and a queued mode change, got queued=%t mode=%t prompt=%q",
			self.queuedTurn.isReplacement, self.queuedTurn.isModeChange, self.queuedTurn.nextMessage,
		)
	}

	for report := range self.turn.events {
		self.takeTurn(report)
	}
	self.finish()

	if self.queuedTurn.isReplacement || self.queuedTurn.isModeChange || self.queuedTurn.nextMessage != "" {
		t.Errorf(
			"expected the whole queue emptied, got queued=%t mode=%t prompt=%q",
			self.queuedTurn.isReplacement, self.queuedTurn.isModeChange, self.queuedTurn.nextMessage,
		)
	}

	if !self.turn.isRunning {
		t.Error("expected the queued prompt to have started a turn")
	}

	for report := range self.turn.events {
		self.takeTurn(report)
	}
	self.finish()

	storedSession, err := store.Read(directory, log.Name())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var messages []string
	for _, event := range storedSession.Events {
		if event.Kind == agent.UserMessage {
			messages = append(messages, event.Text)
		}
	}

	if !slices.Equal(messages, []string{"first", "second"}) {
		t.Errorf("expected the queued prompt alone to follow, got %q", messages)
	}
}

func TestAQueuedModeChangeAloneInjectsItsNotice(t *testing.T) {
	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = log.Close() }()

	self := &Harness{
		agent:  agent.New("", quietProvider{}, nil),
		screen: output.New(&bytes.Buffer{}),
		log:    log,
		mode:   caps.NewMode(caps.Read | caps.Write),
	}

	self.start("first")
	self.toggleCapability(caps.Write)

	for report := range self.turn.events {
		self.takeTurn(report)
	}
	self.finish()

	if self.queuedTurn.isModeChange {
		t.Error("expected the queued mode change to have been taken")
	}

	if !self.turn.isRunning {
		t.Error("expected the mode change to have started a turn of its own")
	}
}
