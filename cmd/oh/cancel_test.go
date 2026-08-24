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
	"crdx.org/io/cmd/oh/turn"
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

func TestControlDStopsATurnBeforeItIsAWayOut(t *testing.T) {
	self := &Harness{screen: output.New(&bytes.Buffer{})}
	editor := edit.NewInput(nil)

	keypress := key.Key{Code: key.Rune, Value: 'd', Mod: key.Ctrl}

	stopped := false

	self.currentTurn = Turn{Stream: testTurnStream(nil, func() { stopped = true }, turn.State{Running: true})}

	if !self.apply(editor, nil, keypress) {
		t.Error("expected ctrl+d during a turn to stop the turn rather than the harness")
	}

	if !stopped || !self.currentTurn.Cancelled() {
		t.Error("expected the turn to have been cancelled, as escape cancels it")
	}

	self.currentTurn = Turn{}

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
	if self.currentTurn.Running() {
		t.Error("expected the first return to do nothing")
	}

	self.apply(editor, history, key.Key{Code: key.Enter})
	if !self.currentTurn.Running() {
		t.Fatal("expected the second return to start a turn")
	}

	for report := range self.currentTurn.Events() {
		self.takeTurn(report)
	}
	self.finish()

	for _, record := range self.events {
		if record.Kind == agent.UserMessageEvent && record.Text == "carry on" {
			return
		}
	}

	t.Error("expected the configured prompt")
}

func TestAcceptedInputCanImmediatelyBeRecalled(t *testing.T) {
	self := &Harness{currentTurn: Turn{Stream: testTurnStream(nil, func() {}, turn.State{Running: true})}}
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
	interruptedEvents := self.currentTurn.Events()

	self.apply(editor, history, key.Key{Code: key.Rune, Value: 'x', Mod: key.Ctrl})
	self.apply(editor, history, key.Key{Code: key.Rune, Value: 'w'})

	if !self.currentTurn.Cancelled() {
		t.Fatal("expected the capability change to interrupt the turn")
	}

	for report := range interruptedEvents {
		self.takeTurn(report)
	}
	self.finish()

	if !self.currentTurn.Running() {
		t.Fatal("expected the capability change to start a replacement turn")
	}

	for report := range self.currentTurn.Events() {
		self.takeTurn(report)
	}
	self.finish()

	var messages []string
	for _, record := range self.events {
		if record.Kind == agent.UserMessageEvent {
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
	interruptedEvents := self.currentTurn.Events()

	self.apply(editor, history, key.Key{Code: key.Enter})
	if self.currentTurn.Cancelled() {
		t.Error("expected the first return to leave the turn running")
	}

	self.apply(editor, history, key.Key{Code: key.Enter})
	if !self.currentTurn.Cancelled() {
		t.Error("expected the second return to cancel the turn")
	}

	for report := range interruptedEvents {
		self.takeTurn(report)
	}
	self.finish()

	if !self.currentTurn.Running() {
		t.Fatal("expected a continuation to start after the interrupted turn")
	}

	for report := range self.currentTurn.Events() {
		self.takeTurn(report)
	}
	self.finish()

	var messages []string
	var interrupted bool
	for _, record := range self.events {
		if record.Kind == agent.UserMessageEvent {
			messages = append(messages, record.Text)
		}
		interrupted = interrupted || record.Kind == agent.InterruptionEvent
	}

	wantMessages := []string{"first", builtInConfig(t).GetOnWithItMessage}
	if !slices.Equal(messages, wantMessages) {
		t.Errorf("got messages %q, want %q", messages, wantMessages)
	}
	if !interrupted {
		t.Error("expected the replacement to record an interruption")
	}
}

type reasoningThenAnswerProvider struct {
	quietProvider

	sends int
}

func (self *reasoningThenAnswerProvider) Send(ctx context.Context, yield agent.Yield) (agent.Reply, error) {
	self.sends++
	if self.sends == 1 {
		if !yield(agent.Output{Kind: agent.ModelReasoningEvent, Text: "provisional thought"}) {
			return agent.Reply{}, nil
		}
		<-ctx.Done()
		return agent.Reply{}, ctx.Err()
	}

	if !yield(agent.Output{Kind: agent.ModelMessageEvent, Text: "replacement answer"}) ||
		!yield(agent.Output{Kind: agent.ModelMessageEvent, Done: true}) {
		return agent.Reply{}, nil
	}
	return agent.Reply{}, nil
}

type eventsAfterCancellationProvider struct {
	quietProvider
}

func (eventsAfterCancellationProvider) Send(ctx context.Context, yield agent.Yield) (agent.Reply, error) {
	for _, thought := range []string{"first", "second"} {
		if !yield(agent.Output{Kind: agent.ModelReasoningEvent, Text: thought}) ||
			!yield(agent.Output{Kind: agent.ModelReasoningEvent, Done: true}) {
			return agent.Reply{}, nil
		}
	}

	<-ctx.Done()

	for _, message := range []string{"first", "second"} {
		if !yield(agent.Output{Kind: agent.ModelMessageEvent, Text: message}) ||
			!yield(agent.Output{Kind: agent.ModelMessageEvent, Done: true}) {
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
		agent:    agent.New("", eventsAfterCancellationProvider{}, nil),
		screen:   output.New(&bytes.Buffer{}),
		recorder: recordSession(log),
		mode:     caps.NewMode(caps.Read | caps.Write),
	}

	self.start("first")
	events := self.currentTurn.Events()

	thoughts := 0
	for thoughts < 2 {
		report := <-events
		self.takeTurn(report)
		if report.Update.Event != nil && report.Update.Event.Kind == agent.ModelReasoningEvent {
			thoughts++
		}
	}

	self.interruptTurn()

	for report := range events {
		self.takeTurn(report)
	}
	self.finish()

	messages := 0
	for _, record := range self.events {
		if record.Kind == agent.ModelMessageEvent {
			messages++
		}
	}

	if messages != 2 {
		t.Errorf("expected both completed messages to be rendered, got %d", messages)
	}
}

func TestAcceptedReplacementDisappearsWhileCancelledTurnStillRuns(t *testing.T) {
	self := &Harness{
		currentTurn: Turn{Stream: testTurnStream(make(chan TurnEvent), func() {}, turn.State{Running: true})},
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
	if !self.currentTurn.Running() {
		t.Fatal("expected cancelled turn to remain running until its event channel closes")
	}
	if !self.currentTurn.Cancelled() {
		t.Fatal("expected the active turn to be marked cancelled")
	}
	pending := self.queuedTurn.Peek()
	if !pending.Replacement || pending.Message != "dfd" {
		t.Fatalf("expected dfd to exist only as an invisible queued prompt, got queued=%t prompt=%q", pending.Replacement, pending.Message)
	}
}

func TestTheLatestOfTwoRapidReplacementsWins(t *testing.T) {
	cancellations := 0
	self := &Harness{
		currentTurn: Turn{
			Stream: testTurnStream(nil, func() { cancellations++ }, turn.State{Running: true}),
		},
	}
	history := edit.NewHistory("", historyLimit)
	editor := edit.NewInput(history)

	for _, replacement := range []string{"first replacement", "second replacement"} {
		for _, value := range replacement {
			self.apply(editor, history, key.Key{Code: key.Rune, Value: value})
		}
		self.apply(editor, history, key.Key{Code: key.Enter})
	}

	pending := self.queuedTurn.Peek()
	if pending.Message != "second replacement" || !pending.Replacement {
		t.Errorf("unexpected queued turn: %+v", pending)
	}
	if cancellations != 2 {
		t.Errorf("cancelled %d times, want 2", cancellations)
	}
}

func TestReplacementInputCancelsProvisionalReasoningAndStartsTheNextTurn(t *testing.T) {
	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = log.Close() }()

	provider := &reasoningThenAnswerProvider{}
	self := &Harness{
		agent:    agent.New("", provider, nil),
		screen:   output.New(&bytes.Buffer{}),
		recorder: recordSession(log),
		mode:     caps.NewMode(caps.Read | caps.Write),
	}
	history := edit.NewHistory("", historyLimit)
	editor := edit.NewInput(history)

	self.start("first")
	interruptedEvents := self.currentTurn.Events()
	for {
		report := <-interruptedEvents
		self.takeTurn(report)
		if report.Update.Delta != nil && report.Update.Delta.Kind == agent.ModelReasoningEvent {
			break
		}
	}
	for _, value := range "replacement" {
		self.apply(editor, history, key.Key{Code: key.Rune, Value: value})
	}
	self.apply(editor, history, key.Key{Code: key.Enter})

	for report := range interruptedEvents {
		self.takeTurn(report)
	}
	self.finish()
	for report := range self.currentTurn.Events() {
		self.takeTurn(report)
	}
	self.finish()

	var messages []string
	var reasoningEvents int
	for _, event := range self.events {
		if event.Kind == agent.UserMessageEvent || event.Kind == agent.ModelMessageEvent {
			messages = append(messages, event.Text)
		}
		if event.Kind == agent.ModelReasoningEvent {
			reasoningEvents++
		}
	}
	if !slices.Equal(messages, []string{"first", "replacement", "replacement answer"}) {
		t.Errorf("got messages %q", messages)
	}
	if reasoningEvents != 0 {
		t.Errorf("stored %d provisional reasoning events", reasoningEvents)
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
		agent:    agent.New("", quietProvider{}, nil),
		screen:   output.New(&screenOutput),
		recorder: recordSession(log),
		mode:     caps.NewMode(caps.Read | caps.Write),
	}
	history := edit.NewHistory("", historyLimit)
	editor := edit.NewInput(history)

	self.start("first")
	interruptedEvents := self.currentTurn.Events()

	for _, value := range "follow up" {
		self.apply(editor, history, key.Key{Code: key.Rune, Value: value})
	}
	self.apply(editor, history, key.Key{Code: key.Enter})

	for report := range interruptedEvents {
		self.takeTurn(report)
	}
	self.finish()

	if !self.currentTurn.Running() {
		t.Fatal("expected the accepted input to start another turn")
	}

	for report := range self.currentTurn.Events() {
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
		sent = sent || event.Kind == agent.UserMessageEvent && event.Text == "follow up"
		interruptionStored = interruptionStored || event.Kind == agent.InterruptionEvent
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
		agent:    agent.New("", quietProvider{}, nil),
		screen:   output.New(&bytes.Buffer{}),
		recorder: recordSession(log),
		mode:     caps.NewMode(caps.Read | caps.Write),
	}

	self.start("first")
	self.interruptTurn()

	for report := range self.currentTurn.Events() {
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
		if event.Kind == agent.InterruptionEvent {
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
	interruptedEvents := self.currentTurn.Events()

	for _, value := range "follow up" {
		self.apply(editor, history, key.Key{Code: key.Rune, Value: value})
	}
	self.apply(editor, history, key.Key{Code: key.Enter})
	self.apply(editor, history, key.Key{Code: key.Escape})

	for report := range interruptedEvents {
		self.takeTurn(report)
	}
	self.finish()

	if self.currentTurn.Running() {
		t.Error("expected the taken-back replacement to leave no turn running")
	}

	for _, record := range self.events {
		if record.Kind == agent.UserMessageEvent && record.Text == "follow up" {
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

	self.currentTurn = Turn{Stream: testTurnStream(nil, func() { stopped = true }, turn.State{Running: true})}

	if !self.apply(editor, nil, key.Key{Code: key.Rune, Value: 'd', Mod: key.Ctrl}) {
		t.Error("expected ctrl+d during a turn to stop the turn rather than the harness")
	}

	if !stopped || !self.currentTurn.Cancelled() {
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
				currentTurn: Turn{Stream: testTurnStream(nil, func() {}, turn.State{Running: true, Cancelled: test.isCancelled})},
			}
			self.queuedTurn.Replace("later")
			self.queuedTurn.MarkModeChange()

			self.cancelTurn()

			pending := self.queuedTurn.Peek()
			kept := pending.Replacement && pending.ModeChange && pending.Message == "later"
			if kept != test.wantKept {
				t.Errorf(
					"expected the queue kept=%t, got queued=%t mode=%t prompt=%q",
					test.wantKept, pending.Replacement, pending.ModeChange, pending.Message,
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
		agent:    agent.New("", quietProvider{}, nil),
		screen:   output.New(&bytes.Buffer{}),
		recorder: recordSession(log),
		mode:     caps.NewMode(caps.Read | caps.Write),
	}

	self.start("first")
	self.toggleCap(caps.Write)
	self.replaceTurn("second")

	pending := self.queuedTurn.Peek()
	if !pending.Replacement || !pending.ModeChange || pending.Message != "second" {
		t.Fatalf(
			"expected both a queued prompt and a queued mode change, got queued=%t mode=%t prompt=%q",
			pending.Replacement, pending.ModeChange, pending.Message,
		)
	}

	for report := range self.currentTurn.Events() {
		self.takeTurn(report)
	}
	self.finish()

	if !self.queuedTurn.Empty() {
		t.Errorf("expected the whole queue emptied, got %+v", self.queuedTurn.Peek())
	}

	if !self.currentTurn.Running() {
		t.Error("expected the queued prompt to have started a turn")
	}

	for report := range self.currentTurn.Events() {
		self.takeTurn(report)
	}
	self.finish()

	storedSession, err := store.Read(directory, log.Name())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var messages []string
	for _, event := range storedSession.Events {
		if event.Kind == agent.UserMessageEvent {
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
		agent:    agent.New("", quietProvider{}, nil),
		screen:   output.New(&bytes.Buffer{}),
		recorder: recordSession(log),
		mode:     caps.NewMode(caps.Read | caps.Write),
	}

	self.start("first")
	self.toggleCap(caps.Write)

	for report := range self.currentTurn.Events() {
		self.takeTurn(report)
	}
	self.finish()

	if !self.queuedTurn.Empty() {
		t.Error("expected the queued mode change to have been taken")
	}

	if !self.currentTurn.Running() {
		t.Error("expected the mode change to have started a turn of its own")
	}
}
