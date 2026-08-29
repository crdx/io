package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"iter"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/BurntSushi/toml"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/backend"
	"crdx.org/io/cmd/oh/bar"
	"crdx.org/io/cmd/oh/call"
	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/cli"
	"crdx.org/io/cmd/oh/config"
	"crdx.org/io/cmd/oh/cycle"
	"crdx.org/io/cmd/oh/dispatch"
	"crdx.org/io/cmd/oh/dynamic"
	"crdx.org/io/cmd/oh/edit"
	"crdx.org/io/cmd/oh/input"
	"crdx.org/io/cmd/oh/key"
	"crdx.org/io/cmd/oh/link"
	"crdx.org/io/cmd/oh/location"
	"crdx.org/io/cmd/oh/menu"
	"crdx.org/io/cmd/oh/metrics"
	"crdx.org/io/cmd/oh/model"
	"crdx.org/io/cmd/oh/output"
	"crdx.org/io/cmd/oh/painter"
	"crdx.org/io/cmd/oh/prompt"
	"crdx.org/io/cmd/oh/record"
	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/segment/activeModel"
	"crdx.org/io/cmd/oh/segment/activitySpinner"
	"crdx.org/io/cmd/oh/segment/contextUsage"
	"crdx.org/io/cmd/oh/segment/gitBranch"
	"crdx.org/io/cmd/oh/segment/localTime"
	"crdx.org/io/cmd/oh/segment/modeToggle"
	"crdx.org/io/cmd/oh/segment/scrollOverflow"
	"crdx.org/io/cmd/oh/segment/sessionEmoji"
	"crdx.org/io/cmd/oh/segment/sessionName"
	"crdx.org/io/cmd/oh/segment/subUsage"
	"crdx.org/io/cmd/oh/segment/turnCount"
	"crdx.org/io/cmd/oh/segment/turnTimer"
	"crdx.org/io/cmd/oh/segment/workspaceDir"
	"crdx.org/io/cmd/oh/sessions"
	"crdx.org/io/cmd/oh/sessions/picker"
	"crdx.org/io/cmd/oh/shell"
	"crdx.org/io/cmd/oh/skill"
	"crdx.org/io/cmd/oh/slash"
	"crdx.org/io/cmd/oh/snippets"
	"crdx.org/io/cmd/oh/startup"
	"crdx.org/io/cmd/oh/store"
	"crdx.org/io/cmd/oh/store/transcript"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/oh/tty"
	"crdx.org/io/cmd/oh/turn"
	"crdx.org/io/cmd/oh/usage"
	"crdx.org/io/cmd/oh/width"
	"crdx.org/io/cmd/oh/workspace"
	"crdx.org/io/internal/file"
	"crdx.org/io/internal/req"
	"crdx.org/io/internal/sandbox"
	"crdx.org/io/internal/sim"
	"crdx.org/io/internal/stop"
	"crdx.org/io/internal/util/pathutil"
	"crdx.org/io/internal/util/strutil"
	"crdx.org/io/provider/anthropic"
	"crdx.org/io/provider/codex"
	"crdx.org/io/provider/ollama"
	"crdx.org/io/provider/opencodego"
	"crdx.org/io/session"
	"crdx.org/io/tool"
	"crdx.org/io/tool/middleware/truncate"
	"crdx.org/io/toolbox"
	"crdx.org/io/toolbox/bash"
	"crdx.org/io/toolbox/notify"
	"crdx.org/io/toolbox/title"
	"crdx.org/io/toolbox/web"
	"crdx.org/io/wire/openai/chatcompletions"
)

func TestEscapeAtRestDoesNotPanic(t *testing.T) {
	self := &App{screen: output.New(&bytes.Buffer{})}
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
	self := &App{screen: output.New(&bytes.Buffer{})}
	editor := edit.NewInput(nil)

	keypress := key.Key{Code: key.Rune, Value: 'd', Mod: key.Ctrl}

	stopped := false

	self.currentTurn = Turn{Stream: testTurnStream(nil, func(error) { stopped = true }, turn.State{Running: true})}

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

func TestTwoReturnsOnAnEmptyIdleLineSendTheContinueMessage(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	self.continueMessage = "carry on"
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
	self := &App{currentTurn: Turn{Stream: testTurnStream(nil, func(error) {}, turn.State{Running: true})}}
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

func TestTwoReturnsOnAnEmptyLineReplaceTheRunningTurnWithTheContinueMessage(t *testing.T) {
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

	wantMessages := []string{"first", builtInConfig(t).Input.Continue}
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

	self := &App{
		agent:    agent.New("", eventsAfterCancellationProvider{}, nil),
		screen:   output.New(&bytes.Buffer{}),
		recorder: record.New(log),
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

	self.interruptTurn(escapeReason)

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
	self := &App{
		currentTurn: Turn{Stream: testTurnStream(make(chan TurnEvent), func(error) {}, turn.State{Running: true})},
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
	self := &App{
		currentTurn: Turn{
			Stream: testTurnStream(nil, func(error) { cancellations++ }, turn.State{Running: true}),
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
	self := &App{
		agent:    agent.New("", provider, nil),
		screen:   output.New(&bytes.Buffer{}),
		recorder: record.New(log),
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
	self := &App{
		agent:    agent.New("", quietProvider{}, nil),
		screen:   output.New(&screenOutput),
		recorder: record.New(log),
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

func TestTheStopKeyNamesItselfAsTheInterruptionReason(t *testing.T) {
	for name, test := range map[string]struct {
		keypress key.Key
		want     string
	}{
		"escape": {keypress: key.Key{Code: key.Escape}, want: escapeReason},
		"ctrl+d": {keypress: key.Key{Code: key.Rune, Value: 'd', Mod: key.Ctrl}, want: ctrlDReason},
	} {
		t.Run(name, func(t *testing.T) {
			if got := stopKeyReason(test.keypress); got != test.want {
				t.Errorf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestAStoppedTurnIsStoredAsAnInterruption(t *testing.T) {
	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	self := &App{
		agent:    agent.New("", quietProvider{}, nil),
		screen:   output.New(&bytes.Buffer{}),
		recorder: record.New(log),
		mode:     caps.NewMode(caps.Read | caps.Write),
	}

	self.start("first")
	self.interruptTurn(escapeReason)

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
			if event.Text != escapeReason {
				t.Errorf("got reason %q, want the one the turn was stopped with", event.Text)
			}
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
	self := &App{screen: output.New(&bytes.Buffer{})}
	editor := edit.NewInput(nil)

	editor.Apply(key.Key{Code: key.Rune, Value: 'a'}, false)

	stopped := false

	self.currentTurn = Turn{Stream: testTurnStream(nil, func(error) { stopped = true }, turn.State{Running: true})}

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
			self := &App{
				currentTurn: Turn{Stream: testTurnStream(nil, func(error) {}, turn.State{Running: true, Cancelled: test.isCancelled})},
			}
			self.queuedTurn.Replace("later")
			self.queuedTurn.MarkModeChange()

			self.cancelTurn(escapeReason)

			pending := self.queuedTurn.Peek()
			kept := pending.Replacement && pending.ModeChange && pending.Message == "later"
			if kept != test.wantKept {
				t.Errorf(
					"expected the queue kept=%t, got queued=%t mode=%t prompt=%q",
					test.wantKept, pending.Replacement, pending.ModeChange, pending.Message,
				)
			}

			if pending.ModeNotice == test.wantKept {
				t.Errorf(
					"expected the dropped mode change to be left as a notice=%t, got %t",
					!test.wantKept, pending.ModeNotice,
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

	provider := &refusingOnceProvider{}
	self := &App{
		agent:    agent.New("", provider, nil),
		screen:   output.New(&bytes.Buffer{}),
		recorder: record.New(log),
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

	if !slices.ContainsFunc(provider.messages, isReadOnlyNote) {
		t.Errorf("expected the queued prompt to carry the mode change, got %q", provider.messages)
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

	if !slices.Equal(messages, []string{"first", nowReadOnlyNote, "second"}) {
		t.Errorf("expected the notice to be stored before the queued prompt, got %q", messages)
	}
}

func TestAModeChangeDroppedByEscapeIsStillDrawnAndCarried(t *testing.T) {
	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var screenOutput bytes.Buffer
	provider := &refusingOnceProvider{}
	self := &App{
		agent:    agent.New("", provider, nil),
		screen:   output.New(&screenOutput),
		recorder: record.New(log),
		mode:     caps.NewMode(caps.Read | caps.Write),
	}

	self.start("first")
	self.toggleCap(caps.Write)
	self.cancelTurn(escapeReason)

	for report := range self.currentTurn.Events() {
		self.takeTurn(report)
	}
	self.finish()

	if self.currentTurn.Running() {
		t.Error("expected the dropped mode change not to start a turn of its own")
	}

	provider.messages = nil
	self.start("second")

	if drawn := style.Plain(screenOutput.String()); !strings.Contains(drawn, nowReadOnlyNote) {
		t.Errorf("expected the mode change to have been drawn, got %q", drawn)
	}

	for report := range self.currentTurn.Events() {
		self.takeTurn(report)
	}
	self.finish()

	if !slices.ContainsFunc(provider.messages, isReadOnlyNote) {
		t.Errorf("expected the next turn to carry the mode change, got %q", provider.messages)
	}

	if err := log.Close(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

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

	if !slices.Equal(messages, []string{"first", nowReadOnlyNote, "second"}) {
		t.Errorf("expected the notice to be stored before the next prompt, got %q", messages)
	}
}

func TestATransitionLeavesAQueuedModeChangeUnsent(t *testing.T) {
	log, err := store.Create(t.TempDir(), store.Meta{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	defer func() { _ = log.Close() }()

	provider := &refusingOnceProvider{}
	self := &App{
		agent:    agent.New("", provider, nil),
		screen:   output.New(&bytes.Buffer{}),
		recorder: record.New(log),
		mode:     caps.NewMode(caps.Read | caps.Write),
	}

	self.start("first")
	self.toggleCap(caps.Write)

	if err := self.requestTransition(cycle.Transition{Kind: cycle.NewSession}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for report := range self.currentTurn.Events() {
		self.takeTurn(report)
	}
	self.finish()

	if self.currentTurn.Running() {
		t.Error("expected the session being closed to start no further turn")
	}

	if !slices.Equal(provider.messages, []string{"first"}) {
		t.Errorf("expected nothing sent while the session was closing, got %q", provider.messages)
	}
}

type refusingOnceProvider struct {
	failures int
	sent     int
	messages []string
}

func (self *refusingOnceProvider) Configure(string, []tool.Definition)   {}
func (self *refusingOnceProvider) AddToolResults([]agent.ToolCallResult) {}
func (self *refusingOnceProvider) Dump() []json.RawMessage               { return nil }
func (self *refusingOnceProvider) Load([]json.RawMessage)                {}

func (self *refusingOnceProvider) AddUserMessage(text string) {
	self.messages = append(self.messages, text)
}

func (self *refusingOnceProvider) Send(context.Context, agent.Yield) (agent.Reply, error) {
	self.sent++

	if self.sent <= self.failures {
		return agent.Reply{}, errors.New("your prompt was flagged")
	}

	return agent.Reply{}, nil
}

func TestAModeChangeThatFailedIsSaidOnceAndNotRepeated(t *testing.T) {
	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = log.Close() }()

	provider := &refusingOnceProvider{failures: 1}
	self := &App{
		agent:    agent.New("", provider, nil),
		screen:   output.New(&bytes.Buffer{}),
		recorder: record.New(log),
		mode:     caps.NewMode(caps.Read | caps.Write),
	}

	self.toggleCap(caps.Write)

	takeTestTurn := func(message string) {
		self.start(message)
		for report := range self.currentTurn.Events() {
			self.takeTurn(report)
		}
		self.finish()
	}

	takeTestTurn("first")
	takeTestTurn("second")
	takeTestTurn("third")

	if said := countModeNotes(provider.messages, nowReadOnlyNote); said != 1 {
		t.Errorf("expected the mode change to be carried once, said %d times in %q", said, provider.messages)
	}
}

func TestModeChangesAcrossFailedTurnsDoNotAccumulate(t *testing.T) {
	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = log.Close() }()

	provider := &refusingOnceProvider{failures: 3}
	self := &App{
		agent:    agent.New("", provider, nil),
		screen:   output.New(&bytes.Buffer{}),
		recorder: record.New(log),
		mode:     caps.NewMode(caps.Read | caps.Write),
	}

	takeTestTurn := func(message string) []string {
		provider.messages = nil
		self.start(message)
		for report := range self.currentTurn.Events() {
			self.takeTurn(report)
		}
		self.finish()

		return provider.messages
	}

	self.toggleCap(caps.Write)
	takeTestTurn("first")

	self.toggleCap(caps.Git)
	said := takeTestTurn("second")

	if !slices.ContainsFunc(said, containsNote(historyNowReadWriteNote)) {
		t.Fatalf("expected the swap that had just been made, got %q", said)
	}

	if slices.ContainsFunc(said, containsNote(nowReadOnlyNote)) {
		t.Errorf("expected a swap said in an earlier turn not to be said again, got %q", said)
	}
}

const (
	nowReadOnlyNote         = "The workspace is now read-only."
	historyNowReadWriteNote = "The .git directory is now read-write."
)

func containsNote(note string) func(message string) bool {
	return func(message string) bool { return strings.Contains(message, note) }
}

func isReadOnlyNote(message string) bool {
	return strings.Contains(message, nowReadOnlyNote)
}

func countModeNotes(messages []string, note string) int {
	said := 0
	for _, message := range messages {
		if strings.Contains(message, note) {
			said++
		}
	}

	return said
}

func TestPendingInputCanTakeBackAnyMessage(t *testing.T) {
	var pending pendingInput
	pending.add(agent.Event{Kind: agent.UserMessageEvent, Text: "first"}, agent.Event{})
	pending.add(agent.Event{Kind: agent.HarnessMessageEvent, Text: "second"}, agent.Event{})

	pending.takeBack(0)

	if !slices.Equal(pending.texts(), []string{"second"}) || pending.items[0].message.Kind != agent.HarnessMessageEvent {
		t.Errorf("got %+v", pending.items)
	}
}

func TestAQueuedModeChangeCanBeTakenBackBeforeItStarts(t *testing.T) {
	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = log.Close() }()

	self := &App{
		agent:    agent.New("", quietProvider{}, nil),
		screen:   output.New(&bytes.Buffer{}),
		recorder: record.New(log),
		mode:     caps.NewMode(caps.Read | caps.Write),
	}

	self.start("first")
	self.toggleCap(caps.Write)
	self.toggleCap(caps.Write)

	for report := range self.currentTurn.Events() {
		self.takeTurn(report)
	}
	self.finish()

	if self.currentTurn.Running() {
		t.Error("a taken-back mode change started another turn")
	}
	if !self.queuedTurn.Empty() || len(self.pending.items) != 0 {
		t.Errorf("taken-back mode change remained queued: %+v %v", self.queuedTurn.Peek(), self.pending.items)
	}

	storedSession, err := store.Read(directory, log.Name())
	if err != nil {
		t.Fatal(err)
	}
	if !storedSession.CanResume() {
		t.Error("taking back the queued mode change left the session unsafe")
	}
}

func TestAQueuedModeChangeStartsWithItsDisplayedMessage(t *testing.T) {
	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = log.Close() }()

	var screenOutput bytes.Buffer
	self := &App{
		agent:    agent.New("", quietProvider{}, nil),
		screen:   output.New(&screenOutput),
		recorder: record.New(log),
		mode:     caps.NewMode(caps.Read | caps.Write),
	}

	self.start("first")
	self.toggleCap(caps.Write)

	if strings.Contains(screenOutput.String(), workspaceNowReadOnly()) {
		t.Errorf("expected the mode notice after the interrupted turn finishes, got %q", screenOutput.String())
	}

	for report := range self.currentTurn.Events() {
		self.takeTurn(report)
	}
	self.finish()

	if !self.queuedTurn.Empty() {
		t.Error("expected the queued mode change to have been taken")
	}

	if !self.currentTurn.Running() {
		t.Fatal("expected the mode change to have started a turn of its own")
	}

	for report := range self.currentTurn.Events() {
		self.takeTurn(report)
	}
	self.finish()

	modeMessage := workspaceNowReadOnly()
	if count := strings.Count(screenOutput.String(), modeMessage); count != 1 {
		t.Errorf("expected the mode message once, got %d in %q", count, screenOutput.String())
	}

	storedSession, err := store.Read(directory, log.Name())
	if err != nil {
		t.Fatal(err)
	}
	if !storedSession.CanResume() {
		t.Error("expected the completed mode-message turn to be resumable")
	}

	var messages []string
	for _, event := range storedSession.Events {
		if event.Kind == agent.UserMessageEvent {
			messages = append(messages, event.Text)
		}
	}
	if !slices.Equal(messages, []string{"first", modeMessage}) {
		t.Errorf("got user messages %q", messages)
	}
}

var (
	captureSecretPattern = regexp.MustCompile(`(?i)(authorization|bearer[[:space:]]|access_token|refresh_token|api[_-]?key|sk-[a-z0-9]{16,}|eyJ[a-z0-9_-]+\.)`)
	capturePIIPattern    = regexp.MustCompile(`(?i)(?:\b[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}\b|(?:^|[^0-9.])(?:\d{1,3}\.){3}\d{1,3}(?::\d+)?)`)
)

func TestSanitisedWireCapturesContainNoCredentialsOrHostPaths(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("testdata", "captures", "*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no sanitised wire captures")
	}

	for _, path := range paths {
		t.Run(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)), func(t *testing.T) {
			capture, err := os.Open(path) //nolint:gosec // the path comes from a testdata glob
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = capture.Close() }()

			lines := bufio.NewScanner(capture)
			lines.Buffer(nil, 16*1024*1024)
			lineNumber := 0
			for lines.Scan() {
				lineNumber++
				line := lines.Text()
				if captureSecretPattern.MatchString(line) {
					t.Errorf("line %d contains a credential-shaped value", lineNumber)
				}
				if capturePIIPattern.MatchString(line) {
					t.Errorf("line %d contains an email or network address", lineNumber)
				}
				if strings.Contains(line, "/home/") || strings.Contains(line, "Dropbox/proj") {
					t.Errorf("line %d contains a host path", lineNumber)
				}

				var record map[string]any
				if err := json.Unmarshal([]byte(line), &record); err != nil {
					t.Errorf("line %d is not JSON: %v", lineNumber, err)
				} else {
					validateCapturePrivacy(t, record, "record")
				}
			}
			if err := lines.Err(); err != nil {
				t.Fatal(err)
			}
			if lineNumber < 2 {
				t.Errorf("capture has %d records, want a head and at least one exchange", lineNumber)
			}
		})
	}
}

func validateCapturePrivacy(t *testing.T, value any, location string) {
	t.Helper()

	switch typed := value.(type) {
	case map[string]any:
		if instructions, exists := typed["instructions"]; exists && instructions != "<system>" {
			t.Errorf("%s contains an unredacted system prompt", location)
		}
		role, _ := typed["role"].(string)
		if role == "system" && typed["content"] != "<system>" {
			t.Errorf("%s contains an unredacted system message", location)
		}
		if role == "assistant" {
			requireCapturePlaceholder(t, typed, "content", "<answer>", location)
			requireCapturePlaceholder(t, typed, "reasoning", "<reasoning>", location)
			requireCapturePlaceholder(t, typed, "reasoning_content", "<reasoning>", location)
		}
		if role == "tool" {
			requireCapturePlaceholder(t, typed, "content", "<tool-output>", location)
		}
		if workspace, exists := typed["workspace"]; exists && workspace != "<workspace>" {
			t.Errorf("%s contains an unredacted workspace", location)
		}
		if identifier, exists := typed["safety_identifier"]; exists && identifier != "<safety-identifier>" {
			t.Errorf("%s contains an unredacted safety identifier", location)
		}
		typeName, _ := typed["type"].(string)
		switch typeName {
		case "thinking", "thinking_delta":
			requireCapturePlaceholder(t, typed, "thinking", "<reasoning>", location)
		case "summary_text":
			requireCapturePlaceholder(t, typed, "text", "<reasoning>", location)
		case "output_text", "text_delta":
			requireCapturePlaceholder(t, typed, "text", "<answer>", location)
		case "input_json_delta":
			requireCapturePlaceholder(t, typed, "partial_json", "<arguments>", location)
		case "response.output_text.done":
			requireCapturePlaceholder(t, typed, "text", "<answer>", location)
		case "response.reasoning_summary_text.done":
			requireCapturePlaceholder(t, typed, "text", "<reasoning>", location)
		case "response.function_call_arguments.done":
			requireCapturePlaceholder(t, typed, "arguments", "<arguments>", location)
		}
		if role == "" && typeName == "" {
			requireCapturePlaceholder(t, typed, "content", "<answer>", location)
			requireCapturePlaceholder(t, typed, "reasoning", "<reasoning>", location)
			requireCapturePlaceholder(t, typed, "reasoning_content", "<reasoning>", location)
		}
		requireCapturePlaceholder(t, typed, "arguments", "<arguments>", location)
		requireCapturePlaceholder(t, typed, "partial_json", "<arguments>", location)
		requireCapturePlaceholder(t, typed, "output", "<tool-output>", location)
		for key, child := range typed {
			validateCapturePrivacy(t, child, location+"."+key)
		}
	case []any:
		for i, child := range typed {
			validateCapturePrivacy(t, child, location+"["+strconv.Itoa(i)+"]")
		}
	}
}

func requireCapturePlaceholder(
	t *testing.T,
	value map[string]any,
	field string,
	placeholder string,
	location string,
) {
	t.Helper()

	text, exists := value[field].(string)
	if exists && text != "" && text != placeholder {
		t.Errorf("%s.%s contains unredacted content", location, field)
	}
}

type wireCaptureRecord struct {
	Kind     string            `json:"kind"`
	Provider string            `json:"provider"`
	Response []json.RawMessage `json:"response"`
}

func TestSanitisedWireCaptureLifecyclesAreCoveredByScenarios(t *testing.T) {
	capturedFeatures := map[string]struct{}{}
	capturePaths, err := filepath.Glob(filepath.Join("testdata", "captures", "*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range capturePaths {
		capture, err := os.Open(path) //nolint:gosec // the path comes from a testdata glob
		if err != nil {
			t.Fatal(err)
		}

		provider := ""
		lines := bufio.NewScanner(capture)
		lines.Buffer(nil, 16*1024*1024)
		for lines.Scan() {
			var record wireCaptureRecord
			if err := json.Unmarshal(lines.Bytes(), &record); err != nil {
				t.Fatal(err)
			}
			if record.Kind == "capture" {
				provider = record.Provider
			}
			for _, payload := range record.Response {
				addWireLifecycleFeatures(capturedFeatures, provider, payload)
			}
		}
		if err := lines.Err(); err != nil {
			t.Fatal(err)
		}
		_ = capture.Close()
	}

	scenarioFeatures := map[string]struct{}{}
	scenarioPaths, err := filepath.Glob(filepath.Join("testdata", "scenarios", "*.toml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range scenarioPaths {
		scenario := readSessionGoldenScenario(t, path)
		provider := scenario.Provider
		if provider == "chat" {
			provider = "opencode-go"
		}
		for _, turn := range []sessionGoldenTurn{scenario.FirstTurn, scenario.ResumeTurn} {
			for _, response := range turn.Responses {
				for _, payload := range response.Events {
					addWireLifecycleFeatures(scenarioFeatures, provider, json.RawMessage(payload))
				}
			}
		}
	}

	for feature := range capturedFeatures {
		if _, covered := scenarioFeatures[feature]; !covered {
			t.Errorf("captured lifecycle feature %q has no generated scenario", feature)
		}
	}
}

func addWireLifecycleFeatures(features map[string]struct{}, provider string, payload json.RawMessage) {
	var done string
	if json.Unmarshal(payload, &done) == nil && done == "[DONE]" || string(payload) == "[DONE]" {
		features[provider+"/done"] = struct{}{}
		return
	}

	var event map[string]any
	if json.Unmarshal(payload, &event) != nil {
		return
	}

	switch provider {
	case "opencode-go":
		choices, _ := event["choices"].([]any)
		for _, rawChoice := range choices {
			choice, _ := rawChoice.(map[string]any)
			delta, _ := choice["delta"].(map[string]any)
			for _, field := range []string{"content", "reasoning_content", "reasoning", "refusal", "tool_calls"} {
				if value := delta[field]; value != nil && value != "" {
					features["chat/"+field] = struct{}{}
				}
			}
			if reason, _ := choice["finish_reason"].(string); reason != "" {
				features["chat/finish/"+reason] = struct{}{}
			}
		}

	case "anthropic":
		eventType, _ := event["type"].(string)
		switch eventType {
		case "content_block_start", "content_block_delta", "content_block_stop", "message_delta", "message_stop", "error":
			features["anthropic/"+eventType] = struct{}{}
		}
		for _, field := range []string{"content_block", "delta"} {
			value, _ := event[field].(map[string]any)
			if valueType, _ := value["type"].(string); valueType != "" {
				features["anthropic/"+field+"/"+valueType] = struct{}{}
			}
		}

	case "codex":
		eventType, _ := event["type"].(string)
		switch eventType {
		case "response.output_text.delta", "response.refusal.delta", "response.output_text.done", "response.reasoning_summary_text.delta", "response.reasoning_summary_part.done", "response.reasoning_text.delta", "response.reasoning_text.done", "response.output_item.done", "response.completed", "response.done", "response.incomplete", "response.failed", "error":
			features["codex/"+eventType] = struct{}{}
		}
		if eventType == "response.output_item.done" {
			item, _ := event["item"].(map[string]any)
			if itemType, _ := item["type"].(string); itemType != "" {
				features["codex/item/"+itemType] = struct{}{}
			}
		}
		if eventType == "response.reasoning_summary_part.done" {
			part, _ := event["part"].(map[string]any)
			if partType, _ := part["type"].(string); partType != "" {
				features["codex/part/"+partType] = struct{}{}
			}
		}
	}
}

func TestPickerMenuAlignmentMatchesTheGolden(t *testing.T) {
	stream := menu.RenderMenu("Choose your provider:", []string{"ChatGPT", "Anthropic", "OpenCode Go"}, 0)
	compareWithGolden(t, "picker-menu", ".ansi", map[string]func() string{
		"initial frame": func() string { return stream },
	})
	compareWithGolden(t, "picker-menu", ".screen", map[string]func() string{
		"initial frame": func() string {
			return strings.Join(visibleScreen(t, stream, 80), "\n")
		},
	})
}

func TestLongAuthorisationURLMatchesTheGolden(t *testing.T) {
	address := "https://auth.example.test/oauth/authorize?client_id=oh-desktop&code_challenge=abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ&code_challenge_method=S256&redirect_uri=http%3A%2F%2Flocalhost%3A1455%2Fauth%2Fcallback&response_type=code&scope=openid%20profile%20email%20offline_access"
	stream := link.RenderURL(address, address)
	compareWithGolden(t, "authorisation-url", ".ansi", map[string]func() string{
		"long URL": func() string { return stream },
	})
	compareWithGolden(t, "authorisation-url", ".screen", map[string]func() string{
		"long URL": func() string {
			return strings.Join(visibleScreen(t, stream, 80), "\n")
		},
	})
}

func TestCompletionProtocolMatchesTheGolden(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	cachePath := location.GetModelCachePath(os.Getenv(backend.EndpointVariable) != "")
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o700); err != nil { //nolint:gosec // the path is the test's own state directory
		t.Fatal(err)
	}
	cache := checkedModelCache(`{"codex":{"models":[{"id":"gpt-5","efforts":["low","high"],"output":128000}]},"anthropic":{"models":[{"id":"claude-sonnet-5","efforts":["none","high"],"output":128000}]}}`)
	if err := os.WriteFile(cachePath, cache, 0o600); err != nil { //nolint:gosec // the path is the test's own state directory
		t.Fatal(err)
	}

	directory := t.TempDir()
	workspaceDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	writeStoredSession(t, directory, workspaceDir, "older-badger", "2024-01-01T00:00:00Z")
	writeStoredSession(t, directory, workspaceDir, "newer-jaguar", "2025-01-01T00:00:00Z")
	sources := cli.Sources{ModelCachePath: cachePath, SessionsDir: directory, ToolNames: completableToolNames}

	requests := []struct {
		name string
		args []string
	}{
		{name: "options", args: []string{"--complete", "option", ""}},
		{name: "models", args: []string{"--complete", "model", "sonnet"}},
		{name: "models of a provider", args: []string{"--complete", "model", "anthropic/"}},
		{name: "efforts", args: []string{"--complete", "effort", "sonnet@"}},
		{name: "providers", args: []string{"--complete", "provider", ""}},
		{name: "capabilities", args: []string{"--complete", "caps", "rxw"}},
		{name: "tools", args: []string{"--complete", "tool", ""}},
		{name: "sessions", args: []string{"--complete", "session", ""}},
	}

	var output strings.Builder
	for _, request := range requests {
		completions, wanted := cli.Complete(request.args, sources)
		if !wanted {
			t.Fatalf("%s completion request was not recognised", request.name)
		}
		_, _ = output.WriteString("=== " + request.name + " ===\n")
		for _, completion := range completions {
			_, _ = output.WriteString(completion + "\n")
		}
	}

	goldenPath := filepath.Join("testdata", "output", "completion.txt")
	if *updateGoldens {
		if err := os.WriteFile(goldenPath, []byte(output.String()), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath) //nolint:gosec // fixed testdata path
	if err != nil {
		t.Fatal(err)
	}
	if output.String() != string(want) {
		t.Errorf("completion protocol differs from %s\n--- got ---\n%s--- want ---\n%s", goldenPath, output.String(), want)
	}
}

func TestResumeArgumentsMatchTheGolden(t *testing.T) {
	originalArgs := os.Args
	t.Cleanup(func() { os.Args = originalArgs })

	cases := [][]string{
		{"-r"},
		{"--resume"},
		{"-r", "chosen-lobster"},
		{"--resume", "chosen-lobster"},
	}

	var output strings.Builder
	for _, arguments := range cases {
		os.Args = append([]string{"oh"}, arguments...)
		input := cli.Bind()
		fmt.Fprintf(
			&output,
			"%-28q picker=%-5t session=%q\n",
			strings.Join(arguments, " "),
			input.IsSessionPicker,
			input.Session,
		)
	}

	compareArgumentsWithGolden(t, "resume-arguments.txt", output.String())
}

func TestModelArgumentsMatchTheGolden(t *testing.T) {
	originalArgs := os.Args
	t.Cleanup(func() { os.Args = originalArgs })

	cases := [][]string{
		{"-m"},
		{"--model"},
		{"-m", "codex/gpt-5.3-codex@high"},
		{"--model", "codex/gpt-5.3-codex@high"},
		{"-m", "-r"},
	}

	var output strings.Builder
	for _, arguments := range cases {
		os.Args = append([]string{"oh"}, arguments...)
		input := cli.Bind()
		fmt.Fprintf(
			&output,
			"%-32q picker=%-5t model=%q\n",
			strings.Join(arguments, " "),
			input.IsModelPicker,
			input.Model,
		)
	}

	compareArgumentsWithGolden(t, "model-arguments.txt", output.String())
}

func compareArgumentsWithGolden(t *testing.T, name string, got string) {
	t.Helper()

	goldenPath := filepath.Join("testdata", "output", name)
	if *updateGoldens {
		if err := os.WriteFile(goldenPath, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath) //nolint:gosec // fixed testdata path
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("arguments differ from %s\n--- got ---\n%s--- want ---\n%s", goldenPath, got, want)
	}
}

func configFrom(t *testing.T, body string) config.Config {
	t.Helper()

	if body == "" {
		config, err := config.Load("")
		if err != nil {
			t.Fatal(err)
		}

		return config
	}

	path := filepath.Join(t.TempDir(), "config.toml")
	version := fmt.Sprintf("version = %d\n", config.Format)
	if err := os.WriteFile(path, []byte(version+undentConfig(body)), 0o600); err != nil {
		t.Fatal(err)
	}

	config, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	return config
}

func undentConfig(body string) string {
	var output strings.Builder
	for row := range strings.SplitSeq(strings.TrimSpace(body), "\n") {
		output.WriteString(strings.TrimLeft(row, "\t"))
		output.WriteString("\n")
	}

	return output.String()
}

func builtInConfig(t *testing.T) config.Config {
	t.Helper()

	config, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}

	return config
}

func TestWhatWasAskedIsRenderedIntoTheConversation(t *testing.T) {
	for _, isLive := range []bool{true, false} {
		var screenOutput bytes.Buffer

		painter := newTestPainter(output.New(&screenOutput), isLive)
		painter.DrawEvent(agent.Event{Kind: agent.UserMessageEvent, Text: "**weather**\n\n- today"})

		plain := style.Plain(screenOutput.String())
		if !strings.Contains(plain, " weather\n \n • today") {
			t.Errorf("isLive=%v: expected the question's markdown to be rendered, got %q", isLive, plain)
		}

		if strings.Contains(plain, "> ") || strings.Contains(plain, "**") {
			t.Errorf("isLive=%v: expected no literal prompt or markdown markers, got %q", isLive, plain)
		}

		if !strings.HasSuffix(plain, "\n") {
			t.Errorf("isLive=%v: expected the submitted message to finish its line immediately, got %q", isLive, plain)
		}
	}
}

func TestASubmittedMessageHasBackgroundRowsAboveAndBelowIt(t *testing.T) {
	got := style.Plain(painter.RenderSubmittedMessage("hello", 8))
	want := "        \n hello  \n        "

	if got != want {
		t.Errorf("submitted message was %q, want %q", got, want)
	}
}

func TestReplayingACallThatWasNeverAnsweredLeavesNothingRunning(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var screenOutput bytes.Buffer

		testConversation := &App{
			agent:    agent.New("", quietProvider{}, nil),
			screen:   output.New(&screenOutput),
			recorder: record.New(testLog(t)),
		}

		testConversation.restore(&store.Session{
			Events: []agent.Event{
				{Kind: agent.UserMessageEvent, Text: "read them both"},
				{Kind: agent.ToolCallRequestEvent, ID: "1", Name: "read", FallbackRendering: agent.FallbackRendering{Subject: "one.go"}},
				{Kind: agent.ToolCallRequestEvent, ID: "2", Name: "read", FallbackRendering: agent.FallbackRendering{Subject: "two.go"}},
				{Kind: agent.ToolCallResultEvent, ID: "1", Name: "read", Text: "package one"},
			},
		})

		if plain := style.Plain(screenOutput.String()); !strings.Contains(plain, "read two.go –") {
			t.Errorf("expected the unanswered call to be closed on replay, got %q", plain)
		}
	})
}

func TestRestoringAConversationClearsTheTerminalBeforeReplaying(t *testing.T) {
	var screenOutput bytes.Buffer
	self := &App{
		agent:    agent.New("", quietProvider{}, nil),
		screen:   output.NewTerminalOfSize(&screenOutput, 80, 24),
		recorder: record.New(testLog(t)),
	}

	self.restore(&store.Session{Events: []agent.Event{{
		Kind: agent.UserMessageEvent,
		Text: "restored conversation",
	}}})

	const clearTerminal = "\x1b[H\x1b[2J\x1b[3J"
	if !strings.HasPrefix(screenOutput.String(), clearTerminal) {
		t.Errorf("expected the terminal to be cleared before replay, got %q", screenOutput.String())
	}
	if !strings.Contains(style.Plain(screenOutput.String()), "restored conversation") {
		t.Errorf("expected the conversation to be replayed after clearing, got %q", screenOutput.String())
	}
}

func TestRestoringAConversationRestoresStateBeforeReturning(t *testing.T) {
	var restored string
	statefulTool := tool.Implement(
		tool.Definition{Name: "stateful", Description: "", Schema: tool.Schema{}},
		func(struct{}) (string, string) { return "", "" },
	).State("test_state", func(state json.RawMessage) error {
		restored = string(state)
		return nil
	}).Plain(func(context.Context, struct{}) (string, error) { return "", nil })

	var screenOutput bytes.Buffer
	self := &App{
		agent:    agent.New("", quietProvider{}, []tool.Tool{statefulTool}),
		screen:   output.New(&screenOutput),
		recorder: record.New(testLog(t)),
	}
	self.restore(&store.Session{Events: []agent.Event{{
		Kind:  agent.StateChangeEvent,
		Name:  "test_state",
		State: json.RawMessage(`{"answer":42}`),
	}}})

	if restored != `{"answer":42}` {
		t.Errorf("got restored state %q", restored)
	}
}

func TestLastMessageIsTheMostRecentModelMessage(t *testing.T) {
	self := &App{events: []agent.Event{
		{Kind: agent.ModelMessageEvent, Text: "first answer"},
		{Kind: agent.UserMessageEvent, Text: "follow up"},
		{Kind: agent.ModelMessageEvent, Text: "latest answer"},
		{Kind: agent.HarnessMessageEvent, Text: "saved"},
	}}

	message, found := self.getLastMessage()
	if !found || message != "latest answer" {
		t.Errorf("got %q and %t", message, found)
	}
}

func TestLastMessageIsUnavailableBeforeAModelMessage(t *testing.T) {
	self := &App{events: []agent.Event{{Kind: agent.UserMessageEvent, Text: "hello"}}}

	if message, found := self.getLastMessage(); found || message != "" {
		t.Errorf("got %q and %t", message, found)
	}
}

func TestReplayingSaysTheWholeConversationAgain(t *testing.T) {
	var screenOutput bytes.Buffer

	testConversation := &App{
		agent:  agent.New("", quietProvider{}, nil),
		screen: output.New(&screenOutput),
	}

	testConversation.events = []agent.Event{
		{Kind: agent.UserMessageEvent, Text: "what is the weather"},
		{Kind: agent.ModelMessageEvent, Text: "it is raining"},
		{Kind: agent.HarnessMessageEvent, Text: "cancelled"},
	}

	testConversation.replay()

	for _, want := range []string{"what is the weather", "it is raining", "cancelled"} {
		if !strings.Contains(screenOutput.String(), want) {
			t.Errorf("expected %q to be drawn again, got %q", want, screenOutput.String())
		}
	}
}

func TestAReadOfASkillIsDrawnAsTheSkill(t *testing.T) {
	const skillPath = "/skills/golang/SKILL.md"

	tests := map[string]struct {
		tool    string
		path    string
		want    string
		painted string
	}{
		"a read of a skill": {
			tool:    "read",
			path:    skillPath,
			want:    "load " + skillPath,
			painted: style.Skill("golang"),
		},
		"a read of a file": {
			tool:    "read",
			path:    "cmd/oh/draw.go",
			want:    "read cmd/oh/draw.go",
			painted: style.Subject("draw.go"),
		},
		"another tool": {tool: "grep", path: skillPath, want: "grep " + skillPath},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			var screenOutput bytes.Buffer
			callPainter := newTestPainter(output.New(&screenOutput), false)

			callPainter.DrawEvent(agent.Event{
				Kind: agent.ToolCallRequestEvent,
				ID:   "1",
				Name: test.tool,
				FallbackRendering: agent.FallbackRendering{
					Subject:  test.path,
					Emphasis: tool.Emphasis{Kind: tool.EmphasisFocus, Value: path.Base(test.path)},
				},
			})
			callPainter.Close(dynamic.Done)

			if plain := style.Plain(screenOutput.String()); !strings.Contains(plain, test.want) {
				t.Errorf("got %q, want %q", plain, test.want)
			}
			if test.painted != "" && !strings.Contains(screenOutput.String(), test.painted) {
				t.Errorf("got %q, want %q painted", screenOutput.String(), test.painted)
			}
		})
	}
}

func TestTheFileASkillIsKeptInIsNotStoodOut(t *testing.T) {
	var screenOutput bytes.Buffer
	callPainter := newTestPainter(output.New(&screenOutput), false)

	callPainter.DrawEvent(agent.Event{
		Kind: agent.ToolCallRequestEvent,
		ID:   "1",
		Name: "read",
		FallbackRendering: agent.FallbackRendering{
			Subject:  "/skills/golang/SKILL.md",
			Emphasis: tool.Emphasis{Kind: tool.EmphasisFocus, Value: "SKILL.md"},
		},
	})
	callPainter.Close(dynamic.Done)

	if strings.Contains(screenOutput.String(), style.Subject("SKILL.md")) {
		t.Errorf("got %q, want the file left dim", screenOutput.String())
	}
}

func TestWhetherACallChangedAnythingComesFromTheToolOfTheMoment(t *testing.T) {
	var screenOutput bytes.Buffer
	self := &App{
		agent:  agent.New("", quietProvider{}, []tool.Tool{slowTool("write")}),
		screen: output.New(&screenOutput),
	}
	callPainter := self.newPainter(false)

	callPainter.DrawEvent(agent.Event{
		Kind:              agent.ToolCallRequestEvent,
		ID:                "1",
		Name:              "write",
		Arguments:         `{"path":"one.go"}`,
		FallbackRendering: agent.FallbackRendering{ReadOnly: true},
	})
	callPainter.Close(dynamic.Done)

	if want := style.Change("write"); !strings.Contains(screenOutput.String(), want) {
		t.Errorf("got %q, want %q", screenOutput.String(), want)
	}
}

func TestACallToAToolThatIsGoneKeepsWhatWasRecorded(t *testing.T) {
	var screenOutput bytes.Buffer
	self := &App{
		agent:  agent.New("", quietProvider{}, nil),
		screen: output.New(&screenOutput),
	}
	callPainter := self.newPainter(false)

	callPainter.DrawEvent(agent.Event{
		Kind:              agent.ToolCallRequestEvent,
		ID:                "1",
		Name:              "gone",
		FallbackRendering: agent.FallbackRendering{Subject: "one.go", ReadOnly: true},
	})
	callPainter.Close(dynamic.Done)

	if want := style.Call("gone"); !strings.Contains(screenOutput.String(), want) {
		t.Errorf("got %q, want %q", screenOutput.String(), want)
	}
}

func notifyWarning(self *App, text string) {
	self.notify(agent.Event{Kind: agent.HarnessMessageEvent, Text: text, Status: agent.WarningStatus})
}

func TestAHarnessNoticeIsDrawnTheSameLiveAndReplayed(t *testing.T) {
	const notice = "The stored session could not be written (no space left on device)"

	var live strings.Builder
	self := &App{
		agent:    agent.New("", quietProvider{}, nil),
		screen:   output.NewTerminalOfSize(&live, 80, 24),
		recorder: record.New(testLog(t)),
	}

	call := agent.Event{Kind: agent.ToolCallRequestEvent, ID: "1", Name: "read", FallbackRendering: agent.FallbackRendering{Subject: "one.go"}}

	self.currentTurn = Turn{Stream: testRunningTurnStream(), painter: self.newPainter(true)}
	self.events = append(self.events, call)
	self.currentTurn.painter.DrawEvent(call)

	notifyWarning(self, notice)

	self.currentTurn.painter.Close(dynamic.Cancelled)
	self.currentTurn = Turn{}
	self.screen.End()

	var replayOutput strings.Builder
	self.screen = output.NewTerminalOfSize(&replayOutput, 80, 24)
	self.replay()

	if visibleScreen(t, live.String(), 80) == nil ||
		strings.Join(visibleScreen(t, live.String(), 80), "\n") !=
			strings.Join(visibleScreen(t, replayOutput.String(), 80), "\n") {
		t.Errorf(
			"a notice said live leaves a different screen from one replayed\nlive:\n%s\nreplayed:\n%s",
			strings.Join(visibleScreen(t, live.String(), 80), "\n"),
			strings.Join(visibleScreen(t, replayOutput.String(), 80), "\n"),
		)
	}
}

func TestTheWholeConversationIsDrawnTheSameLiveAndReplayed(t *testing.T) {
	tools := []tool.Tool{slowTool("read")}

	var live bytes.Buffer
	self := &App{
		agent:    agent.New("", quietProvider{}, tools),
		screen:   output.New(&live),
		recorder: record.New(testLog(t)),
	}
	livePainter := self.newPainter(true)

	events := []agent.Event{
		{Kind: agent.UserMessageEvent, Text: "**Check** this"},
		{Kind: agent.ModelReasoningEvent, Text: "**Reading**\nLooking at the file. Need care."},
		{Kind: agent.ModelMessageEvent, Text: "The **first** answer.\n"},
		{Kind: agent.ToolCallRequestEvent, ID: "1", Name: "read", Arguments: `{"path":"one.go"}`, FallbackRendering: agent.FallbackRendering{Subject: "old"}},
		{Kind: agent.ToolCallRequestEvent, ID: "2", Name: "gone", FallbackRendering: agent.FallbackRendering{Subject: "two.go"}},
		{Kind: agent.ToolCallResultEvent, ID: "1", Name: "read", Took: 2 * time.Second},
		{Kind: agent.ToolCallResultEvent, ID: "2", Name: "gone", Status: agent.ErrorStatus, Took: 3 * time.Second},
		{Kind: agent.ModelMessageEvent, Text: "Done."},
	}

	for _, event := range events {
		self.events = append(self.events, event)
		livePainter.DrawEvent(event)
	}
	notifyWarning(self, "The stored session could not be written (no space left)")

	unansweredCall := agent.Event{Kind: agent.ToolCallRequestEvent, ID: "3", Name: "read", FallbackRendering: agent.FallbackRendering{Subject: "left.go"}}
	self.events = append(self.events, unansweredCall)
	livePainter.DrawEvent(unansweredCall)
	livePainter.Close(dynamic.Cancelled)
	self.screen.End()

	var replayOutput bytes.Buffer
	self.screen = output.New(&replayOutput)
	self.replay()

	plain := style.Plain(live.String())

	for _, want := range []string{"Check", "Looking at the file.", "Need care.", "first answer.", "one.go", "Done."} {
		if !strings.Contains(plain, want) {
			t.Errorf("expected %q on the screen, got %q", want, plain)
		}
	}
	if !strings.Contains(plain, "Need care.\n\nThe first") {
		t.Errorf("expected a blank line between reasoning and the answer, got %q", plain)
	}
	if self.feedback.message.text != "The stored session could not be written (no space left)" {
		t.Errorf("storage warning was not retained as interface feedback: %+v", self.feedback.message)
	}

	if live.String() != replayOutput.String() {
		t.Errorf("live conversation %q differs from replayed conversation %q", live.String(), replayOutput.String())
	}
}

func TestAThoughtHasMarkdownStripped(t *testing.T) {
	rows := painter.RenderReasoning("## **Checking** `one.go`", 40)
	for i := range rows {
		rows[i] = style.Plain(rows[i])
	}

	if got := strings.Join(rows, "\n"); got != "Checking one.go" {
		t.Errorf("got reasoning %q with markdown stripped", got)
	}
}

func TestAThoughtRunsDirectlyIntoAToolCall(t *testing.T) {
	var screenOutput bytes.Buffer
	callPainter := newTestPainter(output.New(&screenOutput), false)

	callPainter.DrawEvent(agent.Event{Kind: agent.ModelReasoningEvent, Text: "checking"})
	callPainter.DrawEvent(agent.Event{Kind: agent.ToolCallRequestEvent, ID: "1", Name: "read", FallbackRendering: agent.FallbackRendering{Subject: "one.go"}})
	callPainter.Close(dynamic.Cancelled)

	plain := style.Plain(screenOutput.String())
	if !strings.Contains(plain, "checking\nread one.go") {
		t.Errorf("expected no blank line between reasoning and the tool call, got %q", plain)
	}
}

func TestAThoughtWrapsAtWordBoundaries(t *testing.T) {
	rows := painter.RenderReasoning("one two three four", 9)
	for i := range rows {
		rows[i] = style.Plain(rows[i])
	}

	if got := strings.Join(rows, "\n"); got != "one two\nthree\nfour" {
		t.Errorf("got %q, want reasoning wrapped between words", got)
	}
}

func TestAStoredCallIsShownTheWayItsToolShowsItNow(t *testing.T) {
	var screenOutput bytes.Buffer

	current := truncate.Tool(buildSlowTool(
		slowToolBuilder("read").Focuses(func(tool.ToolCall) string { return "one.go" }),
	))
	testConversation := &App{
		agent:  agent.New("", quietProvider{}, []tool.Tool{current}),
		screen: output.New(&screenOutput),
	}

	testConversation.events = []agent.Event{{
		Kind:              agent.ToolCallRequestEvent,
		ID:                "1",
		Name:              "read",
		Arguments:         `{"path":"cmd/oh/one.go"}`,
		FallbackRendering: agent.FallbackRendering{Subject: "one.go:1-400"},
	}}

	testConversation.replay()

	if strings.Contains(screenOutput.String(), "one.go:1-400") {
		t.Errorf("expected the stored rendering to be redrawn, got %q", screenOutput.String())
	}

	want := style.Subtle("cmd/oh/") + style.Subject("one.go")
	if !strings.Contains(screenOutput.String(), want) {
		t.Errorf("expected the stored call to take the tool's current focus, got %q", screenOutput.String())
	}
}

func TestACallWhoseToolIsGoneKeepsWhatItLookedLike(t *testing.T) {
	var screenOutput bytes.Buffer

	testConversation := &App{
		agent:  agent.New("", quietProvider{}, nil),
		screen: output.New(&screenOutput),
	}

	testConversation.events = []agent.Event{{
		Kind:              agent.ToolCallRequestEvent,
		ID:                "1",
		Name:              "divine",
		Arguments:         `{"path":"one.go"}`,
		FallbackRendering: agent.FallbackRendering{Subject: "one.go:1-400"},
	}}

	testConversation.replay()

	if !strings.Contains(screenOutput.String(), "one.go:1-400") {
		t.Errorf("expected what it looked like at the time, got %q", screenOutput.String())
	}
}

type messageCaptureProvider struct {
	messages []string
}

func (self *messageCaptureProvider) Configure(string, []tool.Definition) {}
func (self *messageCaptureProvider) AddUserMessage(message string) {
	self.messages = append(self.messages, message)
}
func (self *messageCaptureProvider) AddToolResults([]agent.ToolCallResult) {}
func (self *messageCaptureProvider) Dump() []json.RawMessage               { return nil }
func (self *messageCaptureProvider) Load([]json.RawMessage)                {}
func (self *messageCaptureProvider) Send(context.Context, agent.Yield) (agent.Reply, error) {
	return agent.Reply{}, nil
}

type quietProvider struct{}

func (quietProvider) Configure(string, []tool.Definition)   {}
func (quietProvider) AddUserMessage(string)                 {}
func (quietProvider) AddToolResults([]agent.ToolCallResult) {}
func (quietProvider) Dump() []json.RawMessage               { return nil }
func (quietProvider) Load([]json.RawMessage)                {}

func (quietProvider) Send(context.Context, agent.Yield) (agent.Reply, error) {
	return agent.Reply{}, nil
}

func testConversation(t *testing.T, screenOutput *bytes.Buffer) *App {
	t.Helper()

	log, err := store.Create(t.TempDir(), store.Meta{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Cleanup(func() { _ = log.Close() })

	backend := quietProvider{}

	return &App{
		agent:           agent.New("", backend, nil),
		screen:          output.New(screenOutput),
		recorder:        record.New(log),
		mode:            caps.NewMode(caps.Read | caps.Write),
		continueMessage: builtInConfig(t).Input.Continue,
	}
}

func completeTurn(self *App) {
	self.start("are you there")

	for report := range self.currentTurn.Events() {
		self.takeTurn(report)
	}

	self.finish()
}

func TestATurnThatFinishedByItselfIsNotCalledCancelled(t *testing.T) {
	var screenOutput bytes.Buffer

	completeTurn(testConversation(t, &screenOutput))

	if strings.Contains(screenOutput.String(), "cancelled") {
		t.Errorf("expected a finished turn to say nothing about cancelling, got %q", screenOutput.String())
	}
}

func TestAStoppedTurnIsNotAnnouncedInTheScrollback(t *testing.T) {
	var screenOutput bytes.Buffer

	self := testConversation(t, &screenOutput)

	self.start("are you there")
	self.currentTurn.SetCancelled(true)
	self.currentTurn.Interrupt(stop.Because(escapeReason))

	for report := range self.currentTurn.Events() {
		self.takeTurn(report)
	}

	self.finish()

	plain := style.Plain(screenOutput.String())
	if strings.Contains(plain, "Interrupted") {
		t.Errorf("expected the interruption to stay out of the scrollback, got %q", plain)
	}

	if strings.Contains(screenOutput.String(), "context canceled") {
		t.Errorf("expected the stop to be reported as a stop, got %q", screenOutput.String())
	}
}

func TestAShellCallIsDrawnAsAShellPrompt(t *testing.T) {
	var screenOutput bytes.Buffer
	callPainter := newTestPainter(output.New(&screenOutput), false)

	callPainter.DrawEvent(agent.Event{Kind: agent.ToolCallRequestEvent, ID: "1", Name: "bash", FallbackRendering: agent.FallbackRendering{Subject: "echo hello"}})
	callPainter.Close(dynamic.Done)

	plain := style.Plain(screenOutput.String())
	if !strings.Contains(plain, "$ echo hello") {
		t.Errorf("got %q, want a shell prompt", plain)
	}
	if strings.Contains(plain, "bash echo hello") {
		t.Errorf("got %q, want no tool name", plain)
	}
}

func TestARedrawDuringATurnHandsTheOpenBlockToTheTurn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var screenOutput bytes.Buffer

		testConversation := &App{
			agent:  agent.New("", quietProvider{}, nil),
			screen: output.New(&screenOutput),
		}

		testConversation.currentTurn = Turn{Stream: testRunningTurnStream(), painter: testConversation.newPainter(true)}

		testConversation.events = []agent.Event{
			{Kind: agent.UserMessageEvent, Text: "read it"},
			{Kind: agent.ToolCallRequestEvent, ID: "1", Name: "read", FallbackRendering: agent.FallbackRendering{Subject: "one.go"}},
		}

		previousPainter := testConversation.currentTurn.painter

		testConversation.redraw()

		if testConversation.currentTurn.painter == previousPainter {
			t.Fatal("expected the turn to be given the painter that drew the replay")
		}

		testConversation.currentTurn.painter.DrawEvent(agent.Event{Kind: agent.ToolCallResultEvent, ID: "1", Name: "read", Took: time.Second})

		if plain := style.Plain(screenOutput.String()); !strings.Contains(plain, "read one.go ✓") {
			t.Errorf("expected the replayed call to be answered on the block that was handed over, got %q", plain)
		}
	})
}

func TestARedrawDuringProvisionalReasoningRestoresTheOpenBlock(t *testing.T) {
	var screenOutput bytes.Buffer
	testConversation := &App{
		agent:  agent.New("", quietProvider{}, nil),
		screen: output.New(&screenOutput),
		events: []agent.Event{{Kind: agent.UserMessageEvent, Text: "think"}},
	}
	testConversation.currentTurn = Turn{Stream: testRunningTurnStream(), painter: testConversation.newPainter(true)}
	testConversation.currentTurn.painter.DrawDelta(agent.Delta{
		Kind: agent.ModelReasoningEvent,
		Text: "provisional thought",
	})

	testConversation.redraw()

	got := testConversation.currentTurn.painter.ProvisionalDelta()
	if got.Kind != agent.ModelReasoningEvent || got.Text != "provisional thought" {
		t.Errorf("got provisional delta %+v", got)
	}
}

func TestAStreamingMermaidDiagramKeepsItsLastValidRendering(t *testing.T) {
	const columns = 100
	var screenOutput bytes.Buffer
	painter := newTestPainter(output.NewTerminalOfSize(&screenOutput, columns, 24), true)

	painter.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: "```mermaid\ngraph LR\nA --> B"})
	valid := strings.Join(visibleScreen(t, screenOutput.String(), columns), "\n")
	if !strings.Contains(valid, "►") || strings.Contains(valid, "graph LR") {
		t.Fatalf("expected the first valid prefix to render, got %q", valid)
	}

	painter.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: "\nB -->"})
	invalid := strings.Join(visibleScreen(t, screenOutput.String(), columns), "\n")
	if invalid != valid {
		t.Errorf("invalid prefix replaced the last valid diagram\nvalid:\n%s\ninvalid:\n%s", valid, invalid)
	}

	painter.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: " C"})
	nextValid := strings.Join(visibleScreen(t, screenOutput.String(), columns), "\n")
	if !strings.Contains(nextValid, "C") || strings.Contains(nextValid, "graph LR") {
		t.Errorf("expected the next valid prefix to replace the cached diagram, got %q", nextValid)
	}
}

func TestARedrawKeepsTheLastValidStreamingMermaidDiagram(t *testing.T) {
	const columns = 100
	var screenOutput bytes.Buffer
	testConversation := &App{screen: output.NewTerminalOfSize(&screenOutput, columns, 24)}
	testConversation.currentTurn = Turn{Stream: testRunningTurnStream(), painter: testConversation.newPainter(true)}

	testConversation.currentTurn.painter.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: "```mermaid\ngraph LR\nA --> B"})
	valid := strings.Join(visibleScreen(t, screenOutput.String(), columns), "\n")
	testConversation.currentTurn.painter.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: "\nB -->"})
	testConversation.redraw()

	redrawn := strings.Join(visibleScreen(t, screenOutput.String(), columns), "\n")
	if redrawn != valid {
		t.Errorf("redraw replaced the last valid diagram\nvalid:\n%s\nredrawn:\n%s", valid, redrawn)
	}
}

func TestACompletedInvalidMermaidDiagramFallsBackToSource(t *testing.T) {
	const columns = 100
	const invalid = "```mermaid\ngraph LR\nA --> B\nB -->\n```"
	var screenOutput bytes.Buffer
	painter := newTestPainter(output.NewTerminalOfSize(&screenOutput, columns, 24), true)

	painter.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: "```mermaid\ngraph LR\nA --> B"})
	painter.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: "\nB -->"})
	painter.DrawEvent(agent.Event{Kind: agent.ModelMessageEvent, Text: invalid})

	completed := strings.Join(visibleScreen(t, screenOutput.String(), columns), "\n")
	if !strings.Contains(completed, "graph LR") || !strings.Contains(completed, "B -->") {
		t.Errorf("expected completed invalid Mermaid to fall back to source, got %q", completed)
	}
}

func TestAnAnswerStreamedIsTheSameAsTheAnswerReplayed(t *testing.T) {
	const answer = "# Findings\n\nThe **first** thing is `read`.\n\n- one\n- two\n"

	var live bytes.Buffer

	livePainter := newTestPainter(output.New(&live), true)

	for _, delta := range deltas(answer, 10) {
		livePainter.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: delta})
	}
	livePainter.DrawEvent(agent.Event{Kind: agent.ModelMessageEvent, Text: answer})
	livePainter.End()

	var replayOutput bytes.Buffer

	replayPainter := newTestPainter(output.New(&replayOutput), false)
	replayPainter.DrawEvent(agent.Event{Kind: agent.ModelMessageEvent, Text: answer})
	replayPainter.End()

	plain := style.Plain(live.String())

	for _, want := range []string{"Findings", "first thing is", "• one", "• two"} {
		if !strings.Contains(plain, want) {
			t.Errorf("expected %q drawn, got %q", want, plain)
		}
	}

	if live.String() != replayOutput.String() {
		t.Errorf("streamed %q, replayed %q", live.String(), replayOutput.String())
	}

	if strings.Contains(live.String(), "**") {
		t.Errorf("expected the markdown to be drawn rather than shown, got %q", live.String())
	}
}

func deltas(text string, count int) []string {
	var pieces []string

	size := (len(text) + count - 1) / count

	for at := 0; at < len(text); at += size {
		pieces = append(pieces, text[at:min(at+size, len(text))])
	}

	return pieces
}

func TestCallEmphasisSourceIsDerivedFromRecordedArguments(t *testing.T) {
	current := tool.Implement(
		tool.Definition{
			Name:        "bash",
			Description: "",
			Schema:      tool.Schema{tool.String("path", "command")},
		},
		func(args fakeArgs) (string, string) {
			subject, _, _ := strings.Cut(args.Path, "\n")
			return subject, ""
		},
	).SyntaxFrom("bash", func(args fakeArgs, _ string) string {
		return args.Path
	}).Plain(func(context.Context, fakeArgs) (string, error) { return "", nil })
	conversation := &App{agent: agent.New("", quietProvider{}, []tool.Tool{current})}

	fallback := describeHarnessCall(conversation, agent.Event{
		Name:      "bash",
		Arguments: `{"path":"cat <<EOF\none\nEOF"}`,
	})

	if got, want := fallback.Emphasis.Source, "cat <<EOF\none\nEOF"; got != want {
		t.Errorf("got source %q, want %q", got, want)
	}
}

func TestWorkspacePrefixIsOmittedFromRenderedCallPaths(t *testing.T) {
	const workspaceDir = "/home/alice/project"

	t.Setenv("HOME", "/home/alice")

	current := tool.Implement(
		tool.Definition{
			Name:        "read",
			Description: "",
			Schema:      tool.Schema{tool.String("path", "file")},
		},
		func(args fakeArgs) (string, string) { return args.Path, pathutil.Shorten(args.Path) },
	).FocusPath().Plain(func(context.Context, fakeArgs) (string, error) { return "", nil })
	testConversation := &App{
		agent:        agent.New("", quietProvider{}, []tool.Tool{current}),
		workspaceDir: workspaceDir,
	}

	fallback := describeHarnessCall(testConversation, agent.Event{
		Name:      "read",
		Arguments: `{"path":"/home/alice/project/cmd/oh/draw.go"}`,
	})

	wantEmphasis := tool.Emphasis{Kind: tool.EmphasisFocus, Value: "draw.go"}
	if fallback.Emphasis != wantEmphasis {
		t.Fatalf("unexpected emphasis %#v", fallback.Emphasis)
	}
	if fallback.Subject != "cmd/oh/draw.go" || fallback.Note != "cmd/oh/draw.go" {
		t.Errorf("got rendering %q and detail %q, want the workspace prefixes omitted", fallback.Subject, fallback.Note)
	}
}

func TestRecordedCallPathsAreShortenedWithTheSameFunction(t *testing.T) {
	t.Setenv("HOME", "/home/alice")

	fallback := describeBareCall("/home/alice/project", agent.Event{
		Name: "removed",
		FallbackRendering: agent.FallbackRendering{
			Subject: "/home/alice/project/file.go",
			Note:    "/home/alice/reference/file.go",
		},
	})

	if fallback.Subject != "file.go" || fallback.Note != "~/reference/file.go" {
		t.Errorf("got subject %q and qualifier %q, want both path prefixes shortened", fallback.Subject, fallback.Note)
	}
}

func TestARefusedCallIsDescribedAgainRatherThanFromTheRecord(t *testing.T) {
	refusing := tool.Implement(
		tool.Definition{
			Name:        "shout",
			Description: "",
			Schema:      tool.Schema{tool.String("message", "what to shout")},
		},
		func(struct{}) (string, string) { return "", "" },
	).Validate(func(struct{}) error {
		return errors.New("not in the mood")
	}).Plain(func(context.Context, struct{}) (string, error) { return "", nil })

	testConversation := &App{
		agent: agent.New("", quietProvider{}, []tool.Tool{refusing}),
	}

	fallback := describeHarnessCall(testConversation, agent.Event{
		Name:              "shout",
		Arguments:         `{"message":"oi"}`,
		FallbackRendering: agent.FallbackRendering{Subject: ""},
	})

	if fallback.Note != "" || fallback.Emphasis != (tool.Emphasis{}) {
		t.Fatalf("unexpected call description %q, %#v", fallback.Note, fallback.Emphasis)
	}
	if fallback.Subject != "oi" {
		t.Errorf("got %q, want the arguments described again from the record", fallback.Subject)
	}
}

func testLog(t *testing.T) *store.Writer {
	t.Helper()

	log, err := store.Create(t.TempDir(), store.Meta{})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = log.Close() })

	return log
}

func TestAnAsideStandsBetweenTheCallsItArrivedAmong(t *testing.T) {
	var screenOutput bytes.Buffer
	painter := newTestPainter(output.NewTerminalOfSize(&screenOutput, 80, 24), true)

	painter.DrawEvent(agent.Event{
		Kind:              agent.ToolCallRequestEvent,
		ID:                "1",
		Name:              "read",
		FallbackRendering: agent.FallbackRendering{Subject: "one.txt"},
	})
	painter.DrawEvent(agent.Event{Kind: agent.HarnessMessageEvent, Text: "something happened"})

	painter.DrawEvent(agent.Event{Kind: agent.ToolCallResultEvent, ID: "1", Took: time.Second})

	rows := visibleScreen(t, screenOutput.String(), 80)
	rows = slices.DeleteFunc(rows, func(row string) bool { return strings.TrimSpace(row) == "" })

	if len(rows) != 2 {
		t.Fatalf("expected the call and the aside on rows of their own, got %q", rows)
	}
	if !strings.Contains(rows[0], "one.txt") || !strings.Contains(rows[0], "✓") {
		t.Errorf("expected the call to keep its result above the aside, got %q", rows[0])
	}
	if !strings.Contains(rows[1], "something happened") {
		t.Errorf("expected the aside under the call, got %q", rows[1])
	}
}

func TestCompletedReasoningBlocksRemainSeparateInTheJournal(t *testing.T) {
	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{})
	if err != nil {
		t.Fatal(err)
	}

	var screenOutput bytes.Buffer
	self := &App{
		screen:   output.New(&screenOutput),
		recorder: record.New(log),
	}
	self.currentTurn = Turn{Stream: testRunningTurnStream(), painter: self.newPainter(true)}

	userMessage := agent.Event{Kind: agent.UserMessageEvent, Text: "think twice"}
	self.takeTurn(TurnEvent{Update: agent.Update{Event: &userMessage}})
	firstDelta := agent.Delta{Kind: agent.ModelReasoningEvent, Text: "**First "}
	self.takeTurn(TurnEvent{Update: agent.Update{Delta: &firstDelta}})
	firstBlock := agent.Event{Kind: agent.ModelReasoningEvent, Text: "**First block**"}
	self.takeTurn(TurnEvent{Update: agent.Update{Event: &firstBlock}})
	secondDelta := agent.Delta{Kind: agent.ModelReasoningEvent, Text: "**Second block**"}
	self.takeTurn(TurnEvent{Update: agent.Update{Delta: &secondDelta}})
	secondBlock := agent.Event{Kind: agent.ModelReasoningEvent, Text: "**Second block**"}
	self.takeTurn(TurnEvent{Update: agent.Update{Event: &secondBlock}})

	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	storedSession, err := store.Read(directory, log.Name())
	if err != nil {
		t.Fatal(err)
	}

	var reasoning []string
	for _, event := range storedSession.Events {
		if event.Kind == agent.ModelReasoningEvent {
			reasoning = append(reasoning, event.Text)
		}
	}
	want := []string{"**First block**", "**Second block**"}
	if !slices.Equal(reasoning, want) {
		t.Errorf("got reasoning blocks %q, want %q", reasoning, want)
	}

	plain := style.Plain(screenOutput.String())
	if !strings.Contains(plain, "First block\nSecond block") {
		t.Errorf("expected separate reasoning lines, got %q", plain)
	}
}

func TestIncompleteReasoningIsErasedBeforeAFailure(t *testing.T) {
	var screenOutput bytes.Buffer
	painter := newTestPainter(output.New(&screenOutput), true)

	painter.DrawDelta(agent.Delta{Kind: agent.ModelReasoningEvent, Text: "half a thought"})
	painter.DrawEvent(agent.Event{Kind: agent.FailureEvent, Text: "stream failed"})
	painter.End()

	plain := style.Plain(screenOutput.String())
	if strings.Contains(plain, "half a thought") {
		t.Errorf("incomplete reasoning reached scrollback: %q", plain)
	}
	if !strings.Contains(plain, "Stream failed") {
		t.Errorf("expected the failure, got %q", plain)
	}
}

func TestDiscardedReasoningLeavesTheSameScreenAsReplay(t *testing.T) {
	var liveOutput bytes.Buffer
	self := &App{
		screen:   output.NewTerminalOfSize(&liveOutput, 80, 24),
		recorder: record.New(testLog(t)),
	}
	self.currentTurn = Turn{Stream: testRunningTurnStream(), painter: self.newPainter(true)}

	completeThought := agent.Event{Kind: agent.ModelReasoningEvent, Text: "complete thought"}
	self.takeTurn(TurnEvent{Update: agent.Update{Event: &completeThought}})
	incompleteThought := agent.Delta{Kind: agent.ModelReasoningEvent, Text: "incomplete thought"}
	self.takeTurn(TurnEvent{Update: agent.Update{Delta: &incompleteThought}})
	failure := agent.Event{Kind: agent.FailureEvent, Text: "stream failed"}
	self.takeTurn(TurnEvent{Update: agent.Update{Event: &failure}})
	self.screen.End()

	var replayOutput bytes.Buffer
	replayPainter := newTestPainter(output.NewTerminalOfSize(&replayOutput, 80, 24), false)
	replayPainter.DrawEvent(completeThought)
	replayPainter.DrawEvent(failure)
	replayPainter.End()

	live := visibleScreen(t, liveOutput.String(), 80)
	replayed := visibleScreen(t, replayOutput.String(), 80)
	if !slices.Equal(live, replayed) {
		t.Errorf("discarded reasoning changed the settled screen\nlive:\n%s\nreplayed:\n%s", strings.Join(live, "\n"), strings.Join(replayed, "\n"))
	}
}

func TestSuccessfulHarnessMessageUsesTheSuccessStyle(t *testing.T) {
	got := painter.NoticeStyle(agent.SuccessStatus)("Copied to clipboard.")
	want := style.Success("Copied to clipboard.")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFixtureOutputsAreCompleteAndOwned(t *testing.T) {
	expected := map[string]string{}
	claimFixtureOutputs(t, expected, "replay input", "testdata/input", ".jsonl", []string{
		".ansi",
		".screen",
		".transcript",
	})
	claimFixtureOutputs(t, expected, "generated scenario", "testdata/scenarios", ".toml", []string{
		".ansi",
		".jsonl",
		".meta.json",
		".requests.jsonl",
		".screen",
		".transcript",
	})
	for name, extensions := range map[string][]string{
		"app-plain-resume":      {".jsonl", ".transcript"},
		"app-plain-turn":        {".jsonl", ".transcript"},
		"authorisation-url":     {".ansi", ".screen"},
		"banner":                {".ansi", ".screen"},
		"clearing":              {".ansi", ".screen"},
		"completion":            {".txt"},
		"config-reload":         {".ansi", ".screen"},
		"default-bar":           {".ansi", ".screen"},
		"feedback":              {".ansi", ".screen", ".txt"},
		"context":               {".prompt"},
		"inputblock":            {".ansi", ".screen"},
		"lifecycle":             {".ansi", ".screen"},
		"line-resize":           {".screen"},
		"streaming-modes":       {".screen"},
		"mermaid-streaming":     {".screen"},
		"mode-takeback":         {".ansi", ".screen"},
		"model-arguments":       {".txt"},
		"new-session":           {".txt"},
		"ordinary-tab":          {".ansi", ".screen"},
		"pending-mode-messages": {".ansi", ".screen"},
		"paste":                 {".ansi", ".screen"},
		"picker-menu":           {".ansi", ".screen"},
		"readline-bindings":     {".ansi", ".screen"},
		"resume-arguments":      {".txt"},
		"resume-mode":           {".ansi"},
		"running":               {".ansi", ".screen"},
		"schedule":              {".ansi", ".screen"},
		"segments":              {".ansi", ".screen"},
		"signal-restoration":    {".ansi"},
		"startup":               {".ansi", ".screen"},
		"startup-sized":         {".ansi", ".screen"},
		"startup-sized-output":  {".ansi", ".screen"},
		"vertical-movement":     {".ansi", ".screen"},
	} {
		claimFixtureName(t, expected, "special replay", name, extensions)
	}

	outputDirectory := filepath.Join("testdata", "output")
	entries, err := os.ReadDir(outputDirectory)
	if err != nil {
		t.Fatal(err)
	}
	actual := map[string]struct{}{}
	for _, entry := range entries {
		if entry.IsDir() {
			t.Errorf("unexpected output directory %s", entry.Name())
			continue
		}
		actual[entry.Name()] = struct{}{}
		if _, exists := expected[entry.Name()]; !exists {
			if *updateGoldens {
				if err := os.Remove(filepath.Join(outputDirectory, entry.Name())); err != nil {
					t.Error(err)
				}
				continue
			}
			t.Errorf("orphaned output %s", entry.Name())
		}
	}

	if *updateGoldens {
		return
	}
	for name, owner := range expected {
		if _, exists := actual[name]; !exists {
			t.Errorf("%s is missing output %s", owner, name)
		}
	}
}

func claimFixtureOutputs(
	t *testing.T,
	expected map[string]string,
	owner string,
	directory string,
	sourceExtension string,
	outputExtensions []string,
) {
	t.Helper()

	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != sourceExtension {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), sourceExtension)
		claimFixtureName(t, expected, owner+" "+entry.Name(), name, outputExtensions)
	}
}

func claimFixtureName(
	t *testing.T,
	expected map[string]string,
	owner string,
	name string,
	extensions []string,
) {
	t.Helper()

	for _, extension := range extensions {
		output := name + extension
		if previousOwner, duplicate := expected[output]; duplicate {
			t.Errorf("%s and %s both own %s", previousOwner, owner, output)
			continue
		}
		expected[output] = owner
	}
}

func TestFixtureSourceNamesAreUnique(t *testing.T) {
	owners := map[string]string{}
	for directory, extension := range map[string]string{
		"testdata/input":     ".jsonl",
		"testdata/scenarios": ".toml",
	} {
		entries, err := os.ReadDir(directory)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != extension {
				continue
			}
			name := strings.TrimSuffix(entry.Name(), extension)
			if previousOwner, duplicate := owners[name]; duplicate {
				t.Errorf("duplicate fixture name %q in %s and %s", name, previousOwner, directory)
			}
			owners[name] = directory
		}
	}

	if len(owners) == 0 {
		t.Fatal("no fixture sources")
	}
}

func TestTestdataContainsNoPersonalOrSecretMaterial(t *testing.T) {
	patterns := map[string]*regexp.Regexp{
		"absolute home path": regexp.MustCompile(`/(?:home|Users)/[^\s"\\]+`),
		"credential":         regexp.MustCompile(`(?i)(?:authorization|bearer[[:space:]]+[A-Za-z0-9._~+/-]{12,}|access_token|refresh_token|api[_-]?key|sk-[A-Za-z0-9]{12,}|eyJ[A-Za-z0-9_-]{12,}\.)`),
		"email address":      regexp.MustCompile(`(?i)\b[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}\b`),
		"host address":       regexp.MustCompile(`(?:^|[^0-9.])(?:\d{1,3}\.){3}\d{1,3}(?::\d+)?`),
		"UUID":               regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\b`),
	}

	err := filepath.WalkDir("testdata", func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		contents, err := os.ReadFile(path) //nolint:gosec // the path comes from walking testdata
		if err != nil {
			return err
		}
		for name, pattern := range patterns {
			if pattern.Match(contents) {
				t.Errorf("%s contains %s", path, name)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMain(testingMain *testing.M) {
	sandbox.Init()
	unsetInheritedStateDirectory()
	os.Exit(testingMain.Run())
}

func unsetInheritedStateDirectory() {
	if err := os.Unsetenv(location.StateDirVariable); err != nil {
		panic(err)
	}
}

func TestAResumedConversationDrawsItsRecordedMode(t *testing.T) {
	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{Model: "gpt"})
	if err != nil {
		t.Fatal(err)
	}

	recordedCaps := caps.Read | caps.Shell | caps.Git
	if err := log.Event(caps.ModeEvent(recordedCaps)); err != nil {
		t.Fatal(err)
	}
	if err := log.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	storedSession, err := store.Read(directory, log.Name())
	if err != nil {
		t.Fatal(err)
	}
	restoredCaps, err := sessions.OpeningCaps(caps.Read|caps.Shell|caps.Write, false, storedSession)
	if err != nil {
		t.Fatal(err)
	}

	resumedHarness := &App{mode: caps.NewMode(restoredCaps)}
	modeSegment, err := modeToggle.New(resumedHarness.grantedCaps, resumedHarness.isPrefixPending)(nil)
	if err != nil {
		t.Fatal(err)
	}
	passes := map[string]func() string{
		"recorded rxg rather than default rxw": func() string {
			return modeSegment.Render(segment.Context{})
		},
	}

	compareWithGolden(t, "resume-mode", ".ansi", passes)
}

func modeFixture(t *testing.T) (*App, string) {
	t.Helper()

	currentCaps := caps.Read | caps.Write | caps.Shell
	directory := t.TempDir()

	log, err := store.Create(directory, store.Meta{Model: "gpt"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })

	if err := log.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "hello"}); err != nil {
		t.Fatal(err)
	}

	self := &App{
		agent:    agent.New("", quietProvider{}, nil),
		recorder: record.New(log),
		screen:   output.New(&bytes.Buffer{}),
		mode:     caps.NewMode(currentCaps),
	}
	self.settleMode()

	return self, directory
}

func recordedModes(t *testing.T, self *App, directory string) []agent.Event {
	t.Helper()

	storedSession, err := store.Read(directory, self.recorder.Name())
	if err != nil {
		t.Fatal(err)
	}

	var recorded []agent.Event
	for _, event := range storedSession.Events {
		if event.Kind == caps.ModeChange {
			recorded = append(recorded, event)
		}
	}

	return recorded
}

func TestAModeChangeIsWrittenDownOnceItSettles(t *testing.T) {
	self, directory := modeFixture(t)

	self.toggleCap(caps.Git)
	self.settleMode()

	recorded := recordedModes(t, self, directory)
	if len(recorded) != 2 {
		t.Fatalf("expected the opening mode and the change, got %v", recorded)
	}

	want := caps.Read | caps.Write | caps.Shell | caps.Git
	if got, said := caps.LastRecordedMode(recorded); !said || got != want {
		t.Errorf("expected %s, got %s and %t", want.Flags(), got.Flags(), said)
	}
}

func TestACapabilitySwappedBackIsTakenBackRatherThanWrittenDown(t *testing.T) {
	self, directory := modeFixture(t)

	self.toggleCap(caps.Git)
	if len(self.pending.items) != 1 {
		t.Fatalf("expected the change to be shown, got %v", self.pending.items)
	}
	if recorded := recordedModes(t, self, directory); len(recorded) != 1 {
		t.Errorf("pending mode change was written down: %v", recorded)
	}

	self.toggleCap(caps.Git)
	if len(self.pending.items) != 0 {
		t.Errorf("expected the change to be taken back, got %v", self.pending.items)
	}

	self.settleMode()
	if recorded := recordedModes(t, self, directory); len(recorded) != 1 {
		t.Errorf("expected the opening mode alone, got %v", recorded)
	}
}

func TestACapabilitySwappedBackLeavesTheOtherChangesSayingWhatTheySaid(t *testing.T) {
	self, directory := modeFixture(t)

	self.toggleCap(caps.Git)
	self.toggleCap(caps.Write)

	shown := self.pending.items[1].message.Text
	if shown == "" {
		t.Fatal("expected the second change to say something")
	}

	self.toggleCap(caps.Git)
	if again := self.pending.items[0].message.Text; again != shown {
		t.Errorf("expected %q, got %q", shown, again)
	}

	self.settleMode()
	want := caps.Read | caps.Shell
	if got, _ := caps.LastRecordedMode(recordedModes(t, self, directory)); got != want {
		t.Errorf("expected %s, got %s", want.Flags(), got.Flags())
	}
}

func TestAnIdleModeMessageJoinsTheNextTurn(t *testing.T) {
	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = log.Close() }()

	provider := &messageCaptureProvider{}
	var screenOutput bytes.Buffer
	self := &App{
		agent:    agent.New("", provider, nil),
		screen:   output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines),
		recorder: record.New(log),
		mode:     caps.NewMode(caps.Read | caps.Write),
	}
	self.settleMode()

	self.toggleCap(caps.Write)
	modeMessage := workspaceNowReadOnly()
	if !strings.Contains(screenOutput.String(), modeMessage) {
		t.Errorf("pending mode message was not displayed: %q", screenOutput.String())
	}
	if len(provider.messages) != 0 || self.currentTurn.Running() {
		t.Errorf("pending mode message started a turn: messages=%q running=%t", provider.messages, self.currentTurn.Running())
	}

	self.start("next")
	for report := range self.currentTurn.Events() {
		self.takeTurn(report)
	}
	self.finish()

	if !slices.Equal(provider.messages, []string{modeMessage, "next"}) {
		t.Errorf("provider received %q", provider.messages)
	}
	storedSession, err := store.Read(directory, log.Name())
	if err != nil {
		t.Fatal(err)
	}
	if !storedSession.CanResume() {
		t.Error("combined mode-message turn was not resumable")
	}
}

func TestAModeChangeTheSessionClosesOnIsTakenBack(t *testing.T) {
	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{})
	if err != nil {
		t.Fatal(err)
	}

	var screenOutput bytes.Buffer
	self := &App{
		agent:    agent.New("", &messageCaptureProvider{}, nil),
		screen:   output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines),
		recorder: record.New(log),
		mode:     caps.NewMode(caps.Read | caps.Write),
	}
	self.settleMode()

	self.start("first")
	for report := range self.currentTurn.Events() {
		self.takeTurn(report)
	}
	self.finish()

	self.toggleCap(caps.Write)
	if !strings.Contains(screenOutput.String(), workspaceNowReadOnly()) {
		t.Fatalf("the pending mode message was not displayed: %q", screenOutput.String())
	}

	self.dropPendingInput()

	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	for _, row := range visibleScreen(t, screenOutput.String(), replayColumns) {
		if strings.Contains(row, workspaceNowReadOnly()) {
			t.Errorf("the taken-back mode message was left on the screen: %q", row)
		}
	}

	storedSession, err := store.Read(directory, log.Name())
	if err != nil {
		t.Fatal(err)
	}
	if !storedSession.CanResume() {
		t.Error("a session closed on a mode change was not resumable")
	}
	if got := len(storedSession.Events); got != 2 {
		t.Errorf("stored %d events, want the mode it opened in and the message it ran: %v", got, storedSession.Events)
	}
	want := caps.Read | caps.Write
	if got, said := caps.LastRecordedMode(storedSession.Events); !said || got != want {
		t.Errorf("expected the mode the turn ran in, %s, got %s and %t", want.Flags(), got.Flags(), said)
	}
}

func TestAModeChangeSaysItselfInTheScrollback(t *testing.T) {
	var screenOutput bytes.Buffer

	self, _ := modeFixture(t)
	self.screen = output.New(&screenOutput)
	self.toggleCap(caps.Git)
	self.settleMode()

	if !strings.Contains(screenOutput.String(), "The .git directory is now read-write.") {
		t.Errorf("expected the change to be said, got %q", screenOutput.String())
	}
}

func TestPendingModeMessagesAreSeparatedFromStartupAndEachOther(t *testing.T) {
	requireSameVisibleScreen(
		t,
		"messages a turn has taken differ from independently submitted messages",
		sentModeMessagesStream(t),
		submittedModeMessagesStream(),
	)

	compareWithGolden(t, "pending-mode-messages", ".ansi", map[string]func() string{
		"complete interaction":  func() string { return pendingModeMessagesStream(t, 2) },
		"carried by a new turn": func() string { return sentModeMessagesStream(t) },
	})
	compareWithGolden(t, "pending-mode-messages", ".screen", shownPasses(t, map[string]func() string{
		"1 startup":                    func() string { return pendingModeMessagesStream(t, 0) },
		"2 workspace mode message":     func() string { return pendingModeMessagesStream(t, 1) },
		"3 workspace and git messages": func() string { return pendingModeMessagesStream(t, 2) },
	}))
}

func pendingModeMessagesStream(t *testing.T, toggleCount int) string {
	t.Helper()

	return modeMessagesStream(t, toggleCount, false)
}

func sentModeMessagesStream(t *testing.T) string {
	t.Helper()

	return modeMessagesStream(t, 2, true)
}

func modeMessagesStream(t *testing.T, toggleCount int, isSent bool) string {
	t.Helper()

	self, _ := modeFixture(t)
	var screenOutput strings.Builder
	self.screen = output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines)
	self.screen.Line(startup.RenderBanner(time.Millisecond, false, startup.Info{Session: "brave-otter"}, replayColumns, false))

	if toggleCount > 0 {
		self.toggleCap(caps.Write)
	}
	if toggleCount > 1 {
		self.toggleCap(caps.Git)
	}
	if isSent {
		self.settleMode()
	}

	return screenOutput.String()
}

func submittedModeMessagesStream() string {
	var screenOutput strings.Builder
	screen := output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines)
	screen.Line(startup.RenderBanner(time.Millisecond, false, startup.Info{Session: "brave-otter"}, replayColumns, false))
	picasso := painter.New(screen, false, nil, "", defaultStreamingMode)
	picasso.DrawEvent(agent.Event{Kind: agent.UserMessageEvent, Text: workspaceNowReadOnly()})
	picasso.DrawEvent(agent.Event{Kind: agent.UserMessageEvent, Text: "The .git directory is now read-write."})
	return screenOutput.String()
}

func TestTakingBackAModeChangeDrawsWhatItDrewBefore(t *testing.T) {
	completeInteraction := func() string {
		stream := modeTakebackStream(t, 2)
		if strings.Contains(stream, "\x1b[H\x1b[2J") {
			t.Error("taking the mode change back cleared the screen")
		}
		return stream
	}

	compareWithGolden(t, "mode-takeback", ".ansi", map[string]func() string{
		"complete interaction": completeInteraction,
	})
	compareWithGolden(t, "mode-takeback", ".screen", shownPasses(t, map[string]func() string{
		"1 before either chord":  func() string { return modeTakebackStream(t, 0) },
		"2 after ctrl+x s":       func() string { return modeTakebackStream(t, 1) },
		"3 after ctrl+x s twice": func() string { return modeTakebackStream(t, 2) },
	}))
}

func modeTakebackStream(t *testing.T, toggleCount int) string {
	t.Helper()

	self, _ := modeFixture(t)
	var screenOutput strings.Builder
	self.screen = output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines)

	history := edit.NewHistory("", historyLimit)
	editor := edit.NewInput(history)
	self.editor = editor

	self.screen.Line("conversation remains in scrollback")
	self.show(editor)

	for range toggleCount {
		self.handleKeypressAndShowInput(editor, history, key.Key{Code: key.Rune, Value: 'x', Mod: key.Ctrl})
		self.handleKeypressAndShowInput(editor, history, key.Key{Code: key.Rune, Value: 's'})
	}

	return screenOutput.String()
}

func TestMermaidStreamingDrawsWhatItDrewBefore(t *testing.T) {
	compareWithGolden(t, "mermaid-streaming", ".screen", map[string]func() string{
		"1 first valid prefix": func() string {
			return mermaidStreamingScreen(t, "```mermaid\ngraph LR\nA --> B")
		},
		"2 tilde-fenced prefix": func() string {
			return mermaidStreamingScreen(t, "~~~mermaid\ngraph LR\nA --> B")
		},
		"3 completed fence": func() string {
			return mermaidStreamingScreen(t, "```mermaid\ngraph LR\nA --> B\n```")
		},
		"4 quoted prefix": func() string {
			return mermaidStreamingScreen(t, "> ```mermaid\n> graph LR\n> A --> B")
		},
		"5 listed prefix": func() string {
			return mermaidStreamingScreen(t, "- ```mermaid\n  graph LR\n  A --> B")
		},
		"6 invalid extension": func() string {
			return mermaidStreamingScreen(t, "```mermaid\ngraph LR\nA --> B", "\nB -->")
		},
		"7 invalid after redraw": func() string {
			return mermaidStreamingRedrawnScreen(t)
		},
		"8 next valid prefix": func() string {
			return mermaidStreamingScreen(t, "```mermaid\ngraph LR\nA --> B", "\nB -->", " C")
		},
		"9 completed invalid diagram": func() string {
			return completedInvalidMermaidScreen(t)
		},
	})
}

const streamedAnswerTail = "pika."

func streamedAnswerText() string {
	var text strings.Builder
	for text.Len() < 900 {
		text.WriteString("A sentence of a model answer that goes on long enough to back the redrawing off. ")
	}
	text.WriteString("and this is the very last word: " + streamedAnswerTail)

	return text.String()
}

func streamText(painter *Painter, kind agent.Kind) {
	text := streamedAnswerText()
	for at := 0; at < len(text); at += 5 {
		painter.DrawDelta(agent.Delta{Kind: kind, Text: text[at:min(at+5, len(text))]})
	}
}

func interruptedStreamScreen(t *testing.T, kind agent.Kind, streamingMode output.StreamingMode) string {
	t.Helper()

	var screenOutput bytes.Buffer
	painter := newStreamedTestPainter(output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines), true, streamingMode)
	streamText(painter, kind)
	painter.Close(dynamic.Cancelled)

	return shown(t, screenOutput.String(), replayColumns)
}

func streamThenHarnessMessageScreen(t *testing.T, streamingMode output.StreamingMode) string {
	t.Helper()

	var screenOutput bytes.Buffer
	painter := newStreamedTestPainter(output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines), true, streamingMode)
	streamText(painter, agent.ModelMessageEvent)
	painter.DrawEvent(agent.Event{Kind: agent.HarnessMessageEvent, Text: "the turn was interrupted"})

	return shown(t, screenOutput.String(), replayColumns)
}

func everyStreamingMode() map[string]output.StreamingMode {
	return map[string]output.StreamingMode{
		"asap":  output.StreamingModeASAP,
		"line":  output.StreamingModeLine,
		"paced": output.StreamingModePaced,
	}
}

func TestLineStreamingDrawsEveryFrameAcrossANarrowerResize(t *testing.T) {
	compareWithGolden(t, "line-resize", ".screen", map[string]func() string{
		"wide to narrow": func() string { return lineResizeFrames(t) },
	})
}

func lineResizeFrames(t *testing.T) string {
	t.Helper()

	const wideColumns = 36
	const narrowColumns = 18
	const initial = "one two three four five six seven eight nine ten eleven twelve thirteen"

	var screenOutput bytes.Buffer
	wideScreen := output.NewTerminalOfSize(&screenOutput, wideColumns, replayLines)
	widePainter := newStreamedTestPainter(wideScreen, true, output.StreamingModeLine)
	widePainter.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: initial})
	wideRows := visibleScreen(t, screenOutput.String(), wideColumns)

	var frames strings.Builder
	fmt.Fprintf(&frames, "--- before resize at %d columns ---\n%s\n", wideColumns, strings.Join(wideRows, "\n"))

	provisional := widePainter.ProvisionalDelta()
	narrowScreen := output.NewTerminalOfSize(&screenOutput, narrowColumns, replayLines)
	narrowScreen.Reset()
	narrowPainter := newStreamedTestPainter(narrowScreen, true, output.StreamingModeLine)
	narrowPainter.DrawRestoredDelta(provisional, widePainter)
	narrowRows := visibleScreen(t, screenOutput.String(), narrowColumns)
	fmt.Fprintf(&frames, "--- resize frame at %d columns ---\n%s\n", narrowColumns, strings.Join(narrowRows, "\n"))

	if len(narrowRows) < len(wideRows)+2 {
		t.Errorf("expected narrowing to reveal several newly complete rows, wide=%q narrow=%q", wideRows, narrowRows)
	}

	previousRows := narrowRows
	for _, delta := range []string{" fourteen", " fifteen", " sixteen", " seventeen", " eighteen", " nineteen", " twenty"} {
		narrowPainter.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: delta})
		rows := visibleScreen(t, screenOutput.String(), narrowColumns)
		if slices.Equal(rows, previousRows) {
			continue
		}
		if len(rows) != len(previousRows)+1 {
			t.Errorf("delta %q revealed %d rows, want one: before=%q after=%q", delta, len(rows)-len(previousRows), previousRows, rows)
		}
		fmt.Fprintf(&frames, "--- after%q ---\n%s\n", delta, strings.Join(rows, "\n"))
		previousRows = rows
	}

	return strings.TrimSuffix(frames.String(), "\n")
}

func TestAStreamThatStopsStillShowsEverythingThatArrived(t *testing.T) {
	passes := map[string]func() string{}

	for name, streamingMode := range everyStreamingMode() {
		passes[name+" answer closed"] = func() string {
			return interruptedStreamScreen(t, agent.ModelMessageEvent, streamingMode)
		}
		passes[name+" answer then a harness message"] = func() string {
			return streamThenHarnessMessageScreen(t, streamingMode)
		}
		passes[name+" reasoning closed"] = func() string {
			return interruptedStreamScreen(t, agent.ModelReasoningEvent, streamingMode)
		}
	}

	compareWithGolden(t, "streaming-modes", ".screen", passes)
}

func TestNoStreamedAnswerIsLeftOffTheScreenWhenTheStreamStops(t *testing.T) {
	for name, streamingMode := range everyStreamingMode() {
		for ending, drawn := range map[string]func() string{
			"closed":                 func() string { return interruptedStreamScreen(t, agent.ModelMessageEvent, streamingMode) },
			"then a harness message": func() string { return streamThenHarnessMessageScreen(t, streamingMode) },
		} {
			if !strings.Contains(drawn(), streamedAnswerTail) {
				t.Errorf("a %s answer %s dropped the end of what arrived:\n%s", name, ending, drawn())
			}
		}
	}
}

func streamedAnswerScreen(t *testing.T, streamingMode output.StreamingMode, deltas ...string) string {
	t.Helper()

	var screenOutput bytes.Buffer
	painter := newStreamedTestPainter(
		output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines), true, streamingMode,
	)

	for _, delta := range deltas {
		painter.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: delta})
	}

	return shown(t, screenOutput.String(), replayColumns)
}

func TestAnAnswerThatShowsEverythingIsOnScreenBeforeAnythingSettlesIt(t *testing.T) {
	for name, streamingMode := range map[string]output.StreamingMode{
		"asap":  output.StreamingModeASAP,
		"paced": output.StreamingModePaced,
	} {
		if drawn := streamedAnswerScreen(t, streamingMode, "still ", "working"); !strings.Contains(drawn, "still working") {
			t.Errorf("a %s answer was not on screen while it streamed, got:\n%s", name, drawn)
		}
	}
}

func TestALineIsOnScreenOnceTheOneAfterItHasBegun(t *testing.T) {
	drawn := streamedAnswerScreen(t, output.StreamingModeLine, "still ", "working", "\n\nand ", "more")

	if !strings.Contains(drawn, "still working") {
		t.Errorf("expected a settled line to be shown, got:\n%s", drawn)
	}
	if strings.Contains(drawn, "and more") {
		t.Errorf("expected the line still arriving to be held back, got:\n%s", drawn)
	}
}

func TestALineIsOnScreenAsSoonAsItEnds(t *testing.T) {
	drawn := streamedAnswerScreen(t, output.StreamingModeLine, "still ", "working", "\n")

	if !strings.Contains(drawn, "still working") {
		t.Errorf("expected a line to be shown the moment it ended, got:\n%s", drawn)
	}
}

func TestAWrappedLineIsShownAsSoonAsTheNextOneBegins(t *testing.T) {
	for name, kind := range map[string]agent.Kind{
		"answer":    agent.ModelMessageEvent,
		"reasoning": agent.ModelReasoningEvent,
	} {
		var screenOutput bytes.Buffer
		painter := newStreamedTestPainter(output.NewTerminalOfSize(&screenOutput, 12, replayLines), true, output.StreamingModeLine)
		painter.DrawDelta(agent.Delta{Kind: kind, Text: "one two thre"})
		painter.DrawDelta(agent.Delta{Kind: kind, Text: "e"})
		drawn := shown(t, screenOutput.String(), 12)

		if !strings.Contains(drawn, "one two") {
			t.Errorf("expected the complete %s line to be shown, got:\n%s", name, drawn)
		}
		if strings.Contains(drawn, "three") {
			t.Errorf("expected the incomplete %s line to be held back, got:\n%s", name, drawn)
		}
	}
}

func TestCodeIsShownLineByLineWhileADiagramIsShownWhole(t *testing.T) {
	code := streamedAnswerScreen(t, output.StreamingModeLine, "```go\n", "func main() {}\n", "\tmore")
	if !strings.Contains(code, "func main() {}") {
		t.Errorf("expected a settled line of code to be shown, got:\n%s", code)
	}

	diagram := streamedAnswerScreen(t, output.StreamingModeLine, "```mermaid\n", "graph LR\n", "A --> B")
	if !strings.Contains(diagram, "└") {
		t.Errorf("expected a diagram to keep its closing edge, got:\n%s", diagram)
	}
}

func TestALineStillArrivingIsHeldBackUntilSomethingSettlesIt(t *testing.T) {
	if drawn := streamedAnswerScreen(t, output.StreamingModeLine, "still ", "working"); strings.Contains(drawn, "still working") {
		t.Errorf("expected the only line to be held back while it arrived, got:\n%s", drawn)
	}
}

func TestAProvisionalThoughtIsTakenBackWhateverWasDrawnOfIt(t *testing.T) {
	for name, streamingMode := range everyStreamingMode() {
		if drawn := interruptedStreamScreen(t, agent.ModelReasoningEvent, streamingMode); strings.Contains(drawn, "sentence") {
			t.Errorf("expected a %s thought to be taken back, got:\n%s", name, drawn)
		}
	}
}

func mermaidStreamingScreen(t *testing.T, deltas ...string) string {
	t.Helper()
	var screenOutput bytes.Buffer
	painter := newTestPainter(output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines), true)
	for _, delta := range deltas {
		painter.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: delta})
	}
	return shown(t, screenOutput.String(), replayColumns)
}

func mermaidStreamingRedrawnScreen(t *testing.T) string {
	t.Helper()
	var screenOutput bytes.Buffer
	testConversation := &App{screen: output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines)}
	testConversation.currentTurn = Turn{Stream: testRunningTurnStream(), painter: testConversation.newPainter(true)}
	testConversation.currentTurn.painter.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: "```mermaid\ngraph LR\nA --> B"})
	testConversation.currentTurn.painter.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: "\nB -->"})
	testConversation.redraw()
	return shown(t, screenOutput.String(), replayColumns)
}

func completedInvalidMermaidScreen(t *testing.T) string {
	t.Helper()
	const invalid = "```mermaid\ngraph LR\nA --> B\nB -->\n```"
	var screenOutput bytes.Buffer
	painter := newTestPainter(output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines), true)
	painter.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: "```mermaid\ngraph LR\nA --> B"})
	painter.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: "\nB -->"})
	painter.DrawEvent(agent.Event{Kind: agent.ModelMessageEvent, Text: invalid})
	return shown(t, screenOutput.String(), replayColumns)
}

func checkedModelCache(providers string) []byte {
	return fmt.Appendf(
		nil,
		`{"version":3,"checked":%q,"providers":%s}`, time.Now().Format(time.RFC3339), providers,
	)
}

func buildTestBinary(t *testing.T) string {
	t.Helper()

	binary := filepath.Join(t.TempDir(), "oh")
	command := exec.CommandContext(t.Context(), "go", "build", "-o", binary, ".") //nolint:gosec // building the binary under test
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build oh: %v\n%s", err, output)
	}
	return binary
}

func runTestBinary(t *testing.T, binary string, environment []string, arguments ...string) string {
	t.Helper()

	command := exec.CommandContext(t.Context(), binary, arguments...) //nolint:gosec // running the binary under test
	command.Env = environment
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("oh %s: %v\n%s", strings.Join(arguments, " "), err, output)
	}
	return string(output)
}

func testBinaryEnvironment(t *testing.T, stateDirectory string) []string {
	t.Helper()

	return append(
		os.Environ(),
		"HOME="+t.TempDir(),
		"XDG_CONFIG_HOME="+t.TempDir(),
		"XDG_STATE_HOME="+stateDirectory,
	)
}

func TestVersionDispatchRunsThroughTheBinary(t *testing.T) {
	binary := buildTestBinary(t)
	stateDirectory := t.TempDir()

	output := runTestBinary(t, binary, testBinaryEnvironment(t, stateDirectory), "--version")
	if strings.TrimSpace(output) == "" || strings.Count(output, "\n") != 1 {
		t.Errorf("got %q, want one non-empty line", output)
	}
}

func TestModelListDispatchRunsThroughTheBinary(t *testing.T) {
	binary := buildTestBinary(t)
	stateDirectory := t.TempDir()
	cachePath := filepath.Join(stateDirectory, "org.crdx", "oh", "models.json")
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o700); err != nil {
		t.Fatal(err)
	}
	cache := checkedModelCache(`{"codex":{"models":[{"id":"gpt-cli","efforts":["high"],"output":128000}]}}`)
	if err := os.WriteFile(cachePath, cache, 0o600); err != nil {
		t.Fatal(err)
	}

	output := runTestBinary(t, binary, testBinaryEnvironment(t, stateDirectory), "-l")
	if output != "codex/gpt-cli\n" {
		t.Errorf("got %q", output)
	}
}

func TestModelUpdateDispatchRunsThroughTheBinary(t *testing.T) {
	binary := buildTestBinary(t)
	endpoint := sim.New(&sim.Scenario{Model: "fake", Turns: []sim.Turn{{Say: "Hello."}}})
	server := httptest.NewServer(endpoint)
	t.Cleanup(server.Close)

	address := endpoint.Addresses(server.URL)[sim.Messages]
	stateDirectory := t.TempDir()
	environment := append(testBinaryEnvironment(t, stateDirectory), backend.EndpointVariable+"="+address)

	updated := runTestBinary(t, binary, environment, "-u")
	if !strings.Contains(updated, "Stored model list") {
		t.Errorf("update output did not report storage: %q", updated)
	}

	listed := runTestBinary(t, binary, environment, "-l")
	for _, providerName := range model.ProviderNames() {
		if !strings.Contains(listed, providerName+"/fake") {
			t.Errorf("listing omitted %s: %q", providerName, listed)
		}
	}
}

func TestAnEffortWrittenAsAnAliasInTheConfigIsResolved(t *testing.T) {
	modelCachePath := useRoundRobinModelCache(t)
	selections, err := model.ParseRoundRobin(modelCachePath, []string{"sol@off"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if effort := selections[0].Effort; effort != "none" {
		t.Errorf("expected the level rather than the word that asked for it, got %q", effort)
	}
}

func TestWhatSessionTransitionsMakeOfAModelGlob(t *testing.T) {
	compareWithGolden(t, "new-session", ".txt", map[string]func() string{
		"command line selections": func() string { return resolveCommandLineSelections(t) },
		"effort fallback":         resolveNearestEfforts,
		"fork model globs":        func() string { return resolveForkedSessionGlobs(t) },
		"new session model globs": func() string { return resolveNewSessionGlobs(t) },
	})
}

func useCommandLineModelCache(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	path := location.GetModelCachePath(os.Getenv(backend.EndpointVariable) != "")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil { //nolint:gosec // the path is the test's own state directory
		t.Fatal(err)
	}

	data := checkedModelCache(`{` +
		`"codex":{"models":[{"id":"gpt-5.6-sol","efforts":["none","high"],"output":128000}]},` +
		`"opencode-go":{"models":[{"id":"deepseek-v4-pro","efforts":["medium"],"output":128000}]},` +
		`"anthropic":{"models":[` +
		`{"id":"claude-opus-5","efforts":["medium","max"],"output":128000},` +
		`{"id":"claude-sonnet-5","efforts":["low","high"],"output":128000}` +
		`]}}`)
	if err := os.WriteFile(path, data, 0o600); err != nil { //nolint:gosec // the path is the test's own state directory
		t.Fatal(err)
	}

	return path
}

func resolveCommandLineSelections(t *testing.T) string {
	t.Helper()

	path := useCommandLineModelCache(t)
	var written strings.Builder

	for _, selection := range []string{
		"opus-5",
		"opus5",
		"sonnet",
		"sonnet5",
		"sol",
		"deepseek",
		"anthropic/claude-opus-5",
		"opencode/deepseek",
		"anthropic",
		"codex",
		"opus-5@max",
		"sol@off",
		"sonnet@hi",
		"opus-5@high",
		"opus-5@",
		"@high",
		"sol@none@high",
		"claude",
		"nope",
	} {
		providerName, chosen, effort, err := model.ParseSelection(path, selection)
		if err != nil {
			fmt.Fprintf(&written, "%-28q error: %v\n", selection, err)

			continue
		}

		fmt.Fprintf(&written, "%-28q %s/%s@%s\n", selection, providerName, chosen, effort)
	}

	return written.String()
}

func newSessionFixtureChoices() []model.Choice {
	return []model.Choice{
		{Provider: "anthropic", ID: "claude-opus-4-5", EffortLevels: []string{"low", "high"}},
		{Provider: "anthropic", ID: "claude-opus-5", EffortLevels: []string{"medium", "max"}},
		{Provider: "anthropic", ID: "claude-sonnet-4-5", EffortLevels: []string{"medium"}},
		{Provider: "codex", ID: "gpt-5.6-sol", EffortLevels: []string{"none", "high"}},
	}
}

func resolveForkedSessionGlobs(t *testing.T) string {
	t.Helper()

	choices := newSessionFixtureChoices()
	var written strings.Builder

	for _, glob := range []string{"", "opus-5", "opus-5@max", "nope"} {
		transition, err := cycle.ForkedSessionTransition(glob, "medium", choices, "able-dolphin")
		if err != nil {
			fmt.Fprintf(&written, "%-28q error: %v\n", glob, err)

			continue
		}

		fmt.Fprintf(&written, "%-28q %s %s\n", glob, transitionKindName(transition.Kind), strings.Join(transition.Arguments, " "))
	}

	return written.String()
}

func resolveNewSessionGlobs(t *testing.T) string {
	t.Helper()

	choices := newSessionFixtureChoices()
	var written strings.Builder

	for _, glob := range []string{
		"",
		"opus",
		"OPUS",
		"opus-5",
		"anthropic/claude-opus-4-5",
		"sonnet",
		"gpt",
		"opus-5@high",
		"opus-4-5@high",
		"opus-4-5@hi",
		"gpt@off",
		"sonnet@nope",
		"opus-5@",
		"@high",
		"nope",
		"nonsense",
	} {
		transition, err := cycle.NewSessionTransition(glob, "medium", choices)
		if err != nil {
			fmt.Fprintf(&written, "%-28q error: %v\n", glob, err)

			continue
		}

		fmt.Fprintf(&written, "%-28q %s %s\n", glob, transitionKindName(transition.Kind), strings.Join(transition.Arguments, " "))
	}

	return written.String()
}

func transitionKindName(kind cycle.TransitionKind) string {
	switch kind {
	case cycle.Quit:
		return "quit"
	case cycle.NewSession:
		return "new"
	case cycle.ResumeSession:
		return "resume"
	case cycle.Restart:
		return "restart"
	}

	return "unknown"
}

func resolveNearestEfforts() string {
	var written strings.Builder

	for _, available := range [][]string{
		{"low", "high"},
		{"medium"},
		{"none", "minimal"},
		{"xhigh", "max"},
	} {
		for _, current := range []string{"none", "low", "medium", "high", "max", "unrecognised"} {
			fmt.Fprintf(&written, "%-14q of %-20s -> %q\n", current, "{"+strings.Join(available, ",")+"}", model.NearestEffort(current, available))
		}
	}

	return written.String()
}

func useRoundRobinModelCache(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	path := location.GetModelCachePath(os.Getenv(backend.EndpointVariable) != "")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil { //nolint:gosec // the path is the test's own state directory
		t.Fatal(err)
	}

	data := checkedModelCache(`{"codex":{"models":[{"id":"gpt-5.6-sol","efforts":["none","high"],"output":128000}]},"opencode-go":{"models":[{"id":"deepseek-v4-pro","efforts":["high","max"],"output":128000}]},"anthropic":{"models":[{"id":"claude-opus-5","efforts":["high","max"],"output":128000}]}}`)
	if err := os.WriteFile(path, data, 0o600); err != nil { //nolint:gosec // the path is the test's own state directory
		t.Fatal(err)
	}

	return path
}

type Painter = painter.Picasso

const defaultStreamingMode = output.StreamingModeLine

func newTestPainter(screen *output.Screen, isRunning bool) *Painter {
	return newStreamedTestPainter(screen, isRunning, defaultStreamingMode)
}

func newStreamedTestPainter(screen *output.Screen, isRunning bool, streamingMode output.StreamingMode) *Painter {
	return painter.New(screen, isRunning, nil, "", streamingMode)
}

func describeHarnessCall(harness *App, event agent.Event) agent.FallbackRendering {
	return call.Describe(event, harness.agent.Tool, harness.workspaceDir)
}

func describeBareCall(workspaceDir string, event agent.Event) agent.FallbackRendering {
	return call.Describe(event, nil, workspaceDir)
}

func TestTmpWouldShadowAWorkspace(t *testing.T) {
	for _, workspaceDir := range []string{sandbox.TmpDir, filepath.Join(sandbox.TmpDir, "project")} {
		if !workspace.IsShadowed(workspaceDir) {
			t.Errorf("expected %q to be shadowed", workspaceDir)
		}
	}

	for _, workspaceDir := range []string{"/", "/tmp-project"} {
		if workspace.IsShadowed(workspaceDir) {
			t.Errorf("did not expect %q to be shadowed", workspaceDir)
		}
	}
}

func TestAWorkspaceUnderTmpIsRefused(t *testing.T) {
	if err := workspace.Validate(t.TempDir()); !errors.Is(err, workspace.ErrShadowed) {
		t.Errorf("got %v, want the workspace shadowing error", err)
	}
}

func TestAWorkspaceOutsideTmpIsAccepted(t *testing.T) {
	if err := workspace.Validate("/"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestAWorkspaceNamedThroughTmpIsRefused(t *testing.T) {
	alias := filepath.Join(t.TempDir(), "root")
	if err := os.Symlink("/", alias); err != nil {
		t.Fatalf("could not create workspace alias: %v", err)
	}

	if err := workspace.Validate(alias); !errors.Is(err, workspace.ErrShadowed) {
		t.Errorf("got %v, want the workspace shadowing error", err)
	}
}

func TestARelativeXDGStateHomeIsIgnored(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", filepath.Join("inside", "workspace"))

	got := location.GetSessionsDir()
	if !filepath.IsAbs(got) {
		t.Fatalf("got relative state path %q", got)
	}
	if strings.HasPrefix(got, filepath.Join("inside", "workspace")) {
		t.Errorf("used the relative XDG_STATE_HOME in %q", got)
	}
}

func TestAnAbsoluteXDGStateHomeIsUsed(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", root)

	want := filepath.Join(root, "org.crdx", "oh", "sessions")
	if got := location.GetSessionsDir(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTheConfigIsInTheXDGConfigDirectory(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)

	want := filepath.Join(root, "org.crdx", "oh", "config.toml")
	if got := location.GetConfigFile(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTheGlobalContextIsInTheXDGConfigDirectory(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)

	want := filepath.Join(root, "org.crdx", "oh", "SYSTEM.md")
	if got := location.GetGlobalContextPath(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

var prompts = []string{
	"why does the spinner stutter when a tool runs",
	"add support for reasoning traces",
	"the cancelled turn leaves a tool call unanswered\nand the next request fails",
	"rename the harness to oh",
}

func TestPick(t *testing.T) {
	if os.Getenv("RIG") == "" {
		t.Skip("set RIG to drive the picker")
	}

	directory := t.TempDir()
	workspaceDir := "/home/alice/proj/io"

	for _, prompt := range prompts {
		meta := fmt.Appendf(nil, `{"workspaceDir":%q}`, workspaceDir)
		log, err := session.Create(directory, meta, meta)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := log.Event(agent.Event{Kind: agent.UserMessageEvent, Text: prompt}); err != nil {
			t.Fatal(err)
		}

		if err := log.Close(); err != nil {
			t.Fatal(err)
		}
	}

	sessions, err := sessions.Load(directory)
	if err != nil {
		t.Fatal(err)
	}

	chosenSession, err := picker.Choose(sessions, os.Stdin, os.Stdout)
	screen := output.New(os.Stdout)

	switch {
	case errors.Is(err, menu.ErrCancelled):
		screen.Line(style.Cancelled("nothing was chosen"))
	case err != nil:
		t.Fatal(err)
	default:
		screen.Line(style.Result("chose " + chosenSession.Name + ": " + chosenSession.Title))
	}

	screen.End()
}

func TestTheCompleteSystemPromptMatchesTheGolden(t *testing.T) {
	workspace := t.TempDir()
	for name, body := range map[string]string{
		"AGENTS.md":       "Follow the project rules.",
		"AGENTS.local.md": "Prefer the local rules.",
	} {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	root, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()

	globalPath := filepath.Join(t.TempDir(), "SYSTEM.md")
	if err := os.WriteFile(globalPath, []byte("You are the golden test assistant."), 0o600); err != nil {
		t.Fatal(err)
	}

	got, _, err := prompt.Load(prompt.Config{
		GlobalPath:   globalPath,
		Root:         root,
		WorkspaceDir: "/workspace",
		SessionName:  "brave-otter",
		TmpDir:       "/state/tmps/brave-otter",
		HomeDir:      "/state/home",
		CurrentCaps:  caps.Read | caps.Write | caps.Git | caps.Shell,
		ExtraPaths: shell.Paths{
			Read:  []string{"/reference"},
			Write: []string{"/output"},
			Exec:  []string{"/commands"},
		},
		Skills: []skill.Skill{{
			Name:        "golden",
			Description: "Exercise complete prompt assembly.",
			Location:    "/skills/golden/SKILL.md",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got = strings.ReplaceAll(got, "127.0.0.1", "<loopback>")

	goldenPath := filepath.Join("testdata", "output", "context.prompt")
	if *updateGoldens {
		if err := os.WriteFile(goldenPath, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath) //nolint:gosec // fixed testdata path
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("system prompt differs from %s\n--- got ---\n%s\n--- want ---\n%s", goldenPath, got, want)
	}
}

const (
	replayColumns = 100
	replayLines   = 24
	narrowColumns = 40
	tinyColumns   = 12
	oneColumn     = 1
	noColumns     = 0

	turnSoFar    = 9 * time.Second
	idleSoFar    = 4 * time.Second
	sessionSoFar = 2 * time.Minute
	spinnerSoFar = 250 * time.Millisecond

	workspaceMarker   = "/workspace"
	lifecycleScenario = "success@rxw.jsonl"

	retiredAnimalSession = "brave-vulture"
)

var updateGoldens = flag.Bool("update", false, "write what was drawn back to the golden files")

type replayEntry struct {
	Event *agent.Event `json:"event,omitempty"`
}

func TestEveryScenarioShowsWhatItShowedBefore(t *testing.T) {
	for _, journal := range everyJournal(t) {
		t.Run(journal.name, func(t *testing.T) {
			entries := readJournal(t, journal.path)

			compareWithGolden(t, journal.name, ".screen", map[string]func() string{
				"wide":       func() string { return shownAtWidth(t, entries, replayColumns) },
				"narrow":     func() string { return shownAtWidth(t, entries, narrowColumns) },
				"tiny":       func() string { return shownAtWidth(t, entries, tinyColumns) },
				"one column": func() string { return shownAtWidth(t, entries, oneColumn) },
			})
		})
	}
}

func shownAtWidth(t *testing.T, entries []replayEntry, columns int) string {
	t.Helper()

	return shown(t, replayAtWidth(t, entries, columns), columns)
}

func shown(t *testing.T, stream string, columns int) string {
	t.Helper()

	return strings.Join(visibleScreen(t, stream, columns), "\n")
}

func shownPasses(t *testing.T, passes map[string]func() string) map[string]func() string {
	t.Helper()

	shownAt := map[string]func() string{}

	for name, pass := range passes {
		shownAt[name] = func() string { return shown(t, pass(), replayColumns) }
	}

	return shownAt
}

func TestEveryScenarioDrawsWhatItDrewBefore(t *testing.T) {
	for _, journal := range everyJournal(t) {
		t.Run(journal.name, func(t *testing.T) {
			entries := readJournal(t, journal.path)

			compareWithGolden(t, journal.name, ".ansi", map[string]func() string{
				"wide":           func() string { return replayAtWidth(t, entries, replayColumns) },
				"narrow":         func() string { return replayAtWidth(t, entries, narrowColumns) },
				"tiny":           func() string { return replayAtWidth(t, entries, tinyColumns) },
				"unsized":        func() string { return replayAtWidth(t, entries, noColumns) },
				"one column":     func() string { return replayAtWidth(t, entries, oneColumn) },
				"streamed asap":  func() string { return streamIntoBuffer(t, entries, output.StreamingModeASAP) },
				"streamed line":  func() string { return streamIntoBuffer(t, entries, output.StreamingModeLine) },
				"streamed paced": func() string { return streamIntoBuffer(t, entries, output.StreamingModePaced) },
				"plain":          func() string { return replayPlainly(t, entries) },
			})
		})
	}
}

func TestEverySessionIsWrittenDownTheSameWay(t *testing.T) {
	for _, journal := range everyJournal(t) {
		t.Run(journal.name, func(t *testing.T) {
			entries := readJournal(t, journal.path)

			compareWithGolden(t, journal.name, ".transcript", map[string]func() string{
				"written down": func() string { return writeTranscript(t, entries) },
			})
		})
	}
}

func writeTranscript(t *testing.T, entries []replayEntry) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "chat.md")

	recorder, err := transcript.Open(path, transcript.Meta{
		Name:      "brave-otter",
		Model:     "gpt-5.6-sol",
		Effort:    "high",
		Provider:  "codex",
		Workspace: workspaceMarker,
		Started:   transcriptTime,
	})
	if err != nil {
		t.Fatal(err)
	}

	for at, entry := range entries {
		if entry.Event == nil {
			continue
		}

		when := transcriptTime.Add(time.Duration(at) * time.Second)
		if err := recorder.Event(when, *entry.Event); err != nil {
			t.Fatal(err)
		}
	}

	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}

	written, err := os.ReadFile(path) //nolint:gosec // the path is the test's own transcript file
	if err != nil {
		t.Fatal(err)
	}

	return string(written)
}

var transcriptTime = time.Date(2001, time.January, 1, 0, 0, 0, 0, time.UTC)

func TestTheScreenAroundAConversationDrawsWhatItDrewBefore(t *testing.T) {
	entries := readJournal(t, filepath.Join("testdata", "input", lifecycleScenario))

	passes := map[string]func() string{}

	for _, screen := range []struct {
		name string
		open func(*testing.T) *replayRig
	}{
		{name: "", open: newWideRig},
		{name: " on a pipe", open: newPlainRig},
	} {
		passes["under a footer"+screen.name] = func() string {
			return replayUnderFooter(t, screen.open, entries)
		}
		passes["resized"+screen.name] = func() string {
			return replayThenRedraw(t, screen.open, entries, false)
		}
		passes["resized mid-turn"+screen.name] = func() string {
			return replayThenRedraw(t, screen.open, entries, true)
		}
		passes["progress refreshed mid-turn"+screen.name] = func() string {
			return replayThenRefreshProgress(t, screen.open, entries)
		}
		passes["released and kept"+screen.name] = func() string {
			return replayThenRelease(t, screen.open, entries, true)
		}
		passes["released and unused"+screen.name] = func() string {
			return replayThenRelease(t, screen.open, entries, false)
		}
	}

	compareWithGolden(t, "lifecycle", ".screen", shownPasses(t, onATerminal(passes)))

	passes["editing cursor lifecycle"] = drawEditingCursorLifecycle
	compareWithGolden(t, "lifecycle", ".ansi", passes)
}

const signalGoldenProcessVariable = "OH_TEST_SIGNAL_GOLDEN_PROCESS"

func TestFatalSignalTerminalRestorationMatchesTheGolden(t *testing.T) {
	if os.Getenv(signalGoldenProcessVariable) != "" {
		screen := output.NewTerminalOfSize(os.Stdout, replayColumns, replayLines)
		restoreCursor := screen.BeginEditing()
		screen.Footer([]string{footerPrompt}, 0, len(footerPrompt))

		restore := func() {
			restoreTerminalState(
				screen,
				false,
				restoreCursor,
				func() { _, _ = io.WriteString(os.Stdout, "\x1b[23;0t") },
				func() { _, _ = io.WriteString(os.Stdout, key.Disable) },
			)
		}
		stopListening := tty.RestoreOnSignal(restore)
		defer stopListening()

		if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Second)
		return
	}

	//nolint:gosec // rerun this test binary as its signalled subprocess
	command := exec.CommandContext(
		t.Context(), os.Args[0], "-test.run=^TestFatalSignalTerminalRestorationMatchesTheGolden$",
	)
	command.Env = append(os.Environ(), signalGoldenProcessVariable+"=1")
	output, err := command.CombinedOutput()

	var exitError *exec.ExitError
	if !errors.As(err, &exitError) {
		t.Fatalf("the signalled process returned %v, having drawn %q", err, string(output))
	}
	status, ok := exitError.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGTERM {
		t.Fatalf("the process ended with %v, want SIGTERM", exitError.Sys())
	}

	compareWithGolden(t, "signal-restoration", ".ansi", map[string]func() string{
		"fatal signal": func() string { return string(output) },
	})
}

func drawEditingCursorLifecycle() string {
	var drawn strings.Builder
	screen := output.NewTerminalOfSize(&drawn, replayColumns, replayLines)

	restore := screen.BeginEditing()
	screen.Footer([]string{footerPrompt}, 0, len(footerPrompt))
	screen.Release(false)
	restore()

	return drawn.String()
}

func onATerminal(passes map[string]func() string) map[string]func() string {
	kept := map[string]func() string{}

	for name, pass := range passes {
		if !strings.HasSuffix(name, " on a pipe") {
			kept[name] = pass
		}
	}

	return kept
}

func TestATurnStillRunningDrawsWhatItDrewBefore(t *testing.T) {
	entries := readJournal(t, filepath.Join("testdata", "input", lifecycleScenario))

	passes := map[string]func() string{
		"a call still running": func() string { return replayWhileRunning(t, entries) },
		"discarded reasoning":  func() string { return drawDiscardedReasoning(t) },
		"unknown command during answer": func() string {
			return drawUnknownSlashInputDuringStream(t, "/unknown", agent.ModelMessageEvent)
		},
		"unknown command during reasoning": func() string {
			return drawUnknownSlashInputDuringStream(t, "/unknown", agent.ModelReasoningEvent)
		},
		"unknown snippet during reasoning": func() string {
			return drawUnknownSlashInputDuringStream(t, "//unknown", agent.ModelReasoningEvent)
		},
	}

	compareWithGolden(t, "running", ".ansi", passes)

	screenPasses := shownPasses(t, passes)
	screenPasses["help during reasoning"] = func() string { return shownHelpDuringReasoningFrames(t) }
	compareWithGolden(t, "running", ".screen", screenPasses)
}

func drawDiscardedReasoning(t *testing.T) string {
	t.Helper()

	rig := newReplayRig(t, replayColumns)
	rig.chat.currentTurn = Turn{Stream: testRunningTurnStream(), painter: rig.chat.newPainter(true)}

	completeThought := agent.Event{Kind: agent.ModelReasoningEvent, Text: "complete thought"}
	rig.chat.takeTurn(TurnEvent{Update: agent.Update{Event: &completeThought}})
	incompleteThought := agent.Delta{Kind: agent.ModelReasoningEvent, Text: "incomplete thought"}
	rig.chat.takeTurn(TurnEvent{Update: agent.Update{Delta: &incompleteThought}})
	failure := agent.Event{Kind: agent.FailureEvent, Text: "stream failed"}
	rig.chat.takeTurn(TurnEvent{Update: agent.Update{Event: &failure}})
	rig.chat.screen.End()

	return strings.TrimSuffix(rig.drawn(), "\r\n")
}

func drawUnknownSlashInputDuringStream(t *testing.T, message string, kind agent.Kind) string {
	t.Helper()

	writer := &frameRecordingWriter{}
	self := slashCommandFixture(t, caps.Read)
	self.screen = output.NewTerminalOfSize(writer, replayColumns, replayLines)
	self.commands = fixtureSnippetRegistry(t, nil)
	self.currentTurn = Turn{Stream: testRunningTurnStream(), painter: self.newPainter(true)}

	history := edit.NewHistory("", historyLimit)
	editor := edit.NewInput(history)
	self.editor = editor
	editor.SetText(message)
	self.show(editor)
	self.currentTurn.painter.DrawDelta(agent.Delta{Kind: kind, Text: "still working"})
	framesBeforeCommand := len(writer.frames)

	self.handleKeypressAndShowInput(editor, history, key.Key{Code: key.Enter})

	frames := writer.frames[framesBeforeCommand:]
	self.currentTurn.painter.Close(dynamic.Cancelled)
	return strings.Join(frames, "")
}

type journal struct {
	name string
	path string
}

func everyJournal(t *testing.T) []journal {
	t.Helper()

	paths, err := filepath.Glob(filepath.Join("testdata", "input", "*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}

	if len(paths) == 0 {
		t.Fatal("expected the journals to be found")
	}

	journals := make([]journal, 0, len(paths))

	for _, path := range paths {
		name := strings.TrimSuffix(filepath.Base(path), ".jsonl")
		journals = append(journals, journal{name: name, path: path})
	}

	return journals
}

func drawnOnAStoppedClock(t *testing.T, draw func(t *testing.T) string) string {
	t.Helper()

	var drawn string

	synctest.Test(t, func(t *testing.T) { drawn = draw(t) })

	return drawn
}

func compareWithGolden(t *testing.T, name string, suffix string, passes map[string]func() string) {
	t.Helper()

	var drawn strings.Builder

	for _, pass := range slices.Sorted(maps.Keys(passes)) {
		fmt.Fprintf(&drawn, "=== %s ===\n%s\n", pass, strutil.VisibleEscapes(passes[pass]()))
	}

	goldenPath := filepath.Join("testdata", "output", name+suffix)

	if *updateGoldens {
		if err := os.WriteFile(goldenPath, []byte(drawn.String()), 0o600); err != nil {
			t.Fatal(err)
		}

		return
	}

	want, err := os.ReadFile(goldenPath) //nolint:gosec // fixed testdata path
	if err != nil {
		t.Fatalf("%v: write the goldens with `just -f rig.just goldens`", err)
	}

	if drawn.String() != string(want) {
		t.Errorf(
			"%s drew something else; write the goldens again and read the diff\n"+
				"--- drawn ---\n%s\n--- golden ---\n%s",
			name, drawn.String(), want,
		)
	}
}

func readJournal(t *testing.T, path string) []replayEntry {
	t.Helper()

	contents, err := os.ReadFile(path) //nolint:gosec // the path is the test's own journal
	if err != nil {
		t.Fatal(err)
	}

	var entries []replayEntry
	for number, line := range strings.Split(strings.TrimSpace(string(contents)), "\n") {
		var entry replayEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("%s: line %d: %v", path, number+1, err)
		}

		entries = append(entries, entry)
	}

	return entries
}

type replayRig struct {
	chat         *App
	written      *strings.Builder
	workspaceDir string
}

func newReplayRig(t *testing.T, columns int) *replayRig {
	t.Helper()

	return newRig(t, func(written *strings.Builder, workspaceDir string) *output.Screen {
		return output.NewTerminalOfSize(written, columns, replayLines).LinkPathsUnder(workspaceDir)
	})
}

func newWideRig(t *testing.T) *replayRig {
	t.Helper()

	return newReplayRig(t, replayColumns)
}

func newPlainRig(t *testing.T) *replayRig {
	t.Helper()

	return newRig(t, func(written *strings.Builder, workspaceDir string) *output.Screen {
		return output.New(written).LinkPathsUnder(workspaceDir)
	})
}

func newRig(t *testing.T, openScreen func(*strings.Builder, string) *output.Screen) *replayRig {
	t.Helper()

	workspaceDir := layOutWorkspace(t)

	files := file.New(openWorkspaceRootAt(t, workspaceDir), caps.RefuseWrite(caps.NewMode(caps.All())))

	tools := toolbox.Rummage(files, file.NewSnapshots())
	tools = append(
		tools,
		bash.New(
			files,
			func(context.Context) (sandbox.Policy, error) { return sandbox.Policy{}, nil },
		),
		notify.New(),
	)
	tools = append(tools, web.New(func() bool { return true }, sessionGoldenSearcher{})...)

	var written strings.Builder

	screen := openScreen(&written, workspaceDir)

	return &replayRig{
		written:      &written,
		workspaceDir: workspaceDir,
		chat: &App{
			agent:        agent.New("", quietProvider{}, tools),
			screen:       screen,
			workspaceDir: workspaceDir,
			recorder:     record.New(testLog(t)),
		},
	}
}

func (self *replayRig) load(entries []replayEntry) {
	for _, entry := range entries {
		self.chat.events = append(self.chat.events, *entry.Event)
	}
}

func (self *replayRig) drawn() string {
	return strings.ReplaceAll(self.written.String(), self.workspaceDir, workspaceMarker)
}

func replayAtWidth(t *testing.T, entries []replayEntry, columns int) string {
	t.Helper()

	return replayInto(newReplayRig(t, columns), entries)
}

func replayPlainly(t *testing.T, entries []replayEntry) string {
	t.Helper()

	return replayInto(newPlainRig(t), entries)
}

func replayInto(rig *replayRig, entries []replayEntry) string {
	rig.load(entries)
	rig.chat.replay()

	return rig.drawn()
}

func streamIntoBuffer(t *testing.T, entries []replayEntry, streamingMode output.StreamingMode) string {
	t.Helper()

	rig := newReplayRig(t, replayColumns)
	rig.chat.streamingMode = streamingMode
	rig.chat.currentTurn = Turn{Stream: testRunningTurnStream(), painter: rig.chat.newPainter(true)}
	rig.chat.screen.ReportProgress(true)

	for _, entry := range entries {
		event := *entry.Event
		if event.Kind == agent.ModelMessageEvent || event.Kind == agent.ModelReasoningEvent {
			for piece := range deltaSized(event.Text) {
				rig.chat.currentTurn.painter.DrawDelta(agent.Delta{Kind: event.Kind, Text: piece})
			}
		}

		rig.chat.events = append(rig.chat.events, event)
		rig.chat.currentTurn.painter.DrawEvent(event)

		if rig.chat.currentTurn.painter.Stale() {
			rig.chat.redraw()
		}
	}

	rig.chat.currentTurn.painter.Close(dynamic.Done)
	rig.chat.screen.End()
	rig.chat.screen.ReportProgress(false)

	return rig.drawn()
}

func deltaSized(text string) iter.Seq[string] {
	return func(yield func(string) bool) {
		runes := []rune(text)

		for at := 0; at < len(runes); at += deltaRunes {
			if !yield(string(runes[at:min(at+deltaRunes, len(runes))])) {
				return
			}
		}
	}
}

const deltaRunes = 4

func replayUnderFooter(t *testing.T, openRig func(*testing.T) *replayRig, entries []replayEntry) string {
	t.Helper()

	rig := openRig(t)

	for range 2 {
		rig.chat.screen.Footer([]string{footerPrompt, "> and a second row"}, 1, 0)
	}

	rig.load(entries)
	rig.chat.replay()

	return rig.drawn()
}

const footerPrompt = "> ask something else"

func replayThenRedraw(t *testing.T, openRig func(*testing.T) *replayRig, entries []replayEntry, isRunning bool) string {
	t.Helper()

	rig := openRig(t)
	rig.chat.currentTurn.Stream = testTurnStreamForRunning(isRunning)
	rig.load(entriesWhile(entries, isRunning))
	rig.chat.replay()
	rig.chat.redraw()

	if isRunning {
		rig.chat.currentTurn.painter.Close(dynamic.Cancelled)
	}

	return rig.drawn()
}

func entriesWhile(entries []replayEntry, isRunning bool) []replayEntry {
	if !isRunning {
		return entries
	}

	return entriesUpToFirstCall(entries)
}

func replayThenRelease(t *testing.T, openRig func(*testing.T) *replayRig, entries []replayEntry, shouldKeep bool) string {
	t.Helper()

	rig := openRig(t)
	rig.chat.screen.ReportProgress(true)
	rig.chat.screen.ReportProgress(true)
	rig.load(entries)
	rig.chat.replay()
	rig.chat.screen.Footer([]string{footerPrompt}, 0, len(footerPrompt))
	rig.chat.screen.Release(shouldKeep)

	return rig.drawn()
}

func replayThenRefreshProgress(t *testing.T, openRig func(*testing.T) *replayRig, entries []replayEntry) string {
	t.Helper()

	rig := openRig(t)
	rig.chat.screen.ReportProgress(true)
	rig.load(entries)
	rig.chat.replay()
	rig.chat.screen.Footer([]string{footerPrompt}, 0, len(footerPrompt))
	rig.chat.screen.RefreshProgress()

	return rig.drawn()
}

func replayWhileRunning(t *testing.T, entries []replayEntry) string {
	t.Helper()

	var drawn string

	synctest.Test(t, func(t *testing.T) {
		rig := newReplayRig(t, replayColumns)
		rig.chat.currentTurn.Stream = testRunningTurnStream()
		rig.load(entriesUpToFirstCall(entries))
		rig.chat.replay()

		time.Sleep(revealAndSomeFrames)
		synctest.Wait()

		drawn = rig.drawn()

		rig.chat.currentTurn.painter.Close(dynamic.Cancelled)
	})

	return drawn
}

const revealAndSomeFrames = 7 * time.Second

func entriesUpToFirstCall(entries []replayEntry) []replayEntry {
	for at, entry := range entries {
		if entry.Event != nil && entry.Event.Kind == agent.ToolCallRequestEvent {
			return entries[:at+1]
		}
	}

	return entries
}

func openWorkspaceRootAt(t *testing.T, workspaceDir string) *os.Root {
	t.Helper()

	workspaceRoot, err := os.OpenRoot(workspaceDir)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = workspaceRoot.Close() })

	return workspaceRoot
}

func layOutWorkspace(t *testing.T) string {
	t.Helper()

	workspaceDir := filepath.Join(t.TempDir(), "workspace")

	t.Setenv("HOME", workspaceDir)

	if err := os.CopyFS(workspaceDir, os.DirFS(filepath.Join("testdata", "input", "workspace"))); err != nil {
		t.Fatal(err)
	}

	return workspaceDir
}

func TestALiveTurnLeavesTheSameScreenAsAReplayOfIt(t *testing.T) {
	for _, journal := range everyJournal(t) {
		t.Run(journal.name, func(t *testing.T) {
			entries := readJournal(t, journal.path)

			replayed := visibleScreen(t, replayAtWidth(t, entries, replayColumns), replayColumns)

			for name, streamingMode := range everyStreamingMode() {
				live := visibleScreen(t, streamIntoBuffer(t, entries, streamingMode), replayColumns)

				if slices.Equal(replayed, live) {
					continue
				}

				t.Errorf(
					"a live %s turn and a replay of it left different screens\n--- replayed ---\n%s\n--- live ---\n%s",
					name,
					strings.Join(replayed, "\n"), strings.Join(live, "\n"),
				)
			}
		})
	}
}

func TestTheBannerDrawsWhatItDrewBefore(t *testing.T) {
	passes := map[string]func() string{}

	for _, flags := range []string{"", "r", "rw", "rx", "rxw", "rxwg", "rxwgs", "rg", "rs"} {
		grantedCaps, err := caps.Parse(flags)
		if err != nil {
			t.Fatal(err)
		}

		for _, isRunning := range []bool{false, true} {
			name := "caps " + flags + " while waiting"
			if isRunning {
				name = "caps " + flags + " while running"
			}

			passes[name] = func() string {
				return drawnOnAStoppedClock(t, func(t *testing.T) string {
					t.Helper()

					time.Sleep(spinnerSoFar)

					held := &App{mode: caps.NewMode(grantedCaps)}
					held.currentTurn.Stream = testTimedTurnStream(isRunning, time.Now().Add(-turnSoFar), time.Now().Add(-idleSoFar))

					built := goldenBarLayout(t, held)

					return renderBar(built, segment.BottomLeft)
				})
			}
		}
	}

	for name, inputTokens := range map[string]int{
		"context usage low":  5000,
		"context usage high": 182_000,
	} {
		passes[name] = func() string {
			return drawnOnAStoppedClock(t, func(t *testing.T) string {
				t.Helper()

				held := &App{
					mode:      caps.NewMode(caps.All()),
					metrics:   metrics.New(200_000),
					startedAt: time.Now().Add(-sessionSoFar),
					events: []agent.Event{{
						Kind:  agent.ModelMessageEvent,
						Usage: &agent.Usage{InputTokens: inputTokens},
					}},
				}
				held.metrics.Restore(held.events, 0)

				built := goldenBarLayout(t, held)

				return renderBar(built, segment.BottomLeft)
			})
		}
	}

	compareWithGolden(t, "banner", ".ansi", passes)
	compareWithGolden(t, "banner", ".screen", shownPasses(t, passes))
}

func TestTheBarConfiguredByDefaultDrawsWhatItDrewBefore(t *testing.T) {
	t.Setenv("HOME", "/home/tester")

	passes := map[string]func() string{}

	for _, isRunning := range []bool{false, true} {
		state := "waiting"
		if isRunning {
			state = "running"
		}

		for _, position := range segment.Positions {
			passes[position.String()+" while "+state] = func() string {
				return drawnOnAStoppedClock(t, func(t *testing.T) string {
					t.Helper()

					time.Sleep(spinnerSoFar)

					held := &App{
						mode:      caps.NewMode(caps.All()),
						metrics:   metrics.New(200_000),
						startedAt: time.Now().Add(-sessionSoFar),
						events: []agent.Event{{
							Kind:  agent.ModelMessageEvent,
							Usage: &agent.Usage{InputTokens: 42_000},
						}},
					}
					held.metrics.Restore(held.events, 0)
					held.currentTurn.Stream = testTimedTurnStream(
						isRunning,
						time.Now().Add(-turnSoFar),
						time.Now().Add(-idleSoFar),
					)

					layout, err := configFrom(t, "").BuildLayout(
						availableSegments(workspaceMarker, "brave-otter", "gpt-5.6-sol", "high", held),
					)
					if err != nil {
						t.Fatal(err)
					}

					return renderBar(layout, position)
				})
			}
		}
	}

	compareWithGolden(t, "default-bar", ".ansi", passes)
	compareWithGolden(t, "default-bar", ".screen", shownPasses(t, passes))
}

func TestTheStartupLineDrawsWhatItDrewBefore(t *testing.T) {
	line := func(sessionName string, columns int, isTextSizingSupported bool) string {
		event := startup.NewEvent(1500*time.Microsecond, startup.Info{
			Session:       sessionName,
			PromptBytes:   740 + 3*1024,
			ProjectSkills: 3,
			GlobalSkills:  1,
			Snippets:      2,
			ToolBytes:     614,
		})
		return startup.RenderEvent(event, columns, isTextSizingSupported)
	}

	compareWithGolden(t, "startup", ".ansi", map[string]func() string{
		"styled": func() string { return line("brave-otter", replayColumns, false) },
	})

	passes := map[string]func() string{}
	for name, columns := range map[string]int{
		"wide":       replayColumns,
		"narrow":     narrowColumns,
		"tiny":       tinyColumns,
		"one column": oneColumn,
	} {
		passes[name] = func() string { return shown(t, line("brave-otter", columns, false), columns) }
	}
	compareWithGolden(t, "startup", ".screen", passes)

	minimumColumns := 0
	for columns := 1; columns <= replayColumns; columns++ {
		if strings.Contains(line("brave-otter", columns, true), "\x1b]66;") {
			minimumColumns = columns
			break
		}
	}
	if minimumColumns == 0 {
		t.Fatal("sized startup banner never fits")
	}

	compareWithGolden(t, "startup-sized", ".ansi", map[string]func() string{
		"wide with emoji":          func() string { return line("brave-otter", replayColumns, true) },
		"minimum width with emoji": func() string { return line("brave-otter", minimumColumns, true) },
		"below minimum width":      func() string { return line("brave-otter", minimumColumns-1, true) },
		"unknown animal":           func() string { return line("brave-tester", replayColumns, true) },
		"retired animal":           func() string { return line(retiredAnimalSession, replayColumns, true) },
		"no session":               func() string { return line("", replayColumns, true) },
		"unsupported protocol":     func() string { return line("brave-otter", replayColumns, false) },
	})
	compareWithGolden(t, "startup-sized", ".screen", map[string]func() string{
		"wide with emoji": func() string {
			return shown(t, line("brave-otter", replayColumns, true), replayColumns)
		},
		"minimum width with emoji": func() string {
			return shown(t, line("brave-otter", minimumColumns, true), minimumColumns)
		},
		"below minimum width": func() string {
			return shown(t, line("brave-otter", minimumColumns-1, true), minimumColumns-1)
		},
	})

	terminalStream := func(columns int) string {
		var stream strings.Builder
		screen := output.NewTerminalOfSize(&stream, columns, replayLines)
		screen.Line(line("brave-otter", columns, true))
		screen.Line("Following output.")
		screen.End()
		return stream.String()
	}
	compareWithGolden(t, "startup-sized-output", ".ansi", map[string]func() string{
		"wide then following output":            func() string { return terminalStream(replayColumns) },
		"wrapped details then following output": func() string { return terminalStream(minimumColumns) },
		"fallback then following output":        func() string { return terminalStream(minimumColumns - 1) },
	})
	compareWithGolden(t, "startup-sized-output", ".screen", map[string]func() string{
		"wide then following output": func() string {
			return shown(t, terminalStream(replayColumns), replayColumns)
		},
		"wrapped details then following output": func() string {
			return shown(t, terminalStream(minimumColumns), minimumColumns)
		},
		"fallback then following output": func() string {
			return shown(t, terminalStream(minimumColumns-1), minimumColumns-1)
		},
	})
}

func TestTheInputBlockDrawsWhatItDrewBefore(t *testing.T) {
	frames := map[string]edit.Frame{
		"one row": {
			Rows: []string{"> what is the weather"}, Row: 0, Column: 21,
		},
		"reverse history search": {
			Rows: []string{"git diff"}, Row: 0, Column: 8,
			IsSearching: true, SearchQuery: "git",
		},
		"scrolled both ways": {
			Rows: []string{"> the third row", "> the fourth row"}, Row: 1, Column: 16,
			HiddenLinesAbove: 2, HiddenLinesBelow: 7,
		},
	}

	passes := map[string]func() string{}
	shownPassesAtWidth := map[string]func() string{}

	for _, width := range []int{80, 40, 20} {
		for name, frame := range frames {
			passName := fmt.Sprintf("%s at %d columns", name, width)

			passes[passName] = func() string {
				return drawnOnAStoppedClock(t, func(t *testing.T) string {
					t.Helper()

					time.Sleep(spinnerSoFar)

					held := &App{mode: caps.NewMode(caps.All())}
					held.currentTurn.Stream = testTimedTurnStream(true, time.Now().Add(-turnSoFar), time.Time{})

					built := goldenBarLayout(t, held)

					held.barConfiguration = bar.NewConfiguration(nil, built)

					block := input.Block{
						Top: input.Ruler{
							Left:   held.renderBar(segment.TopLeft, frame),
							Center: held.renderBar(segment.TopCenter, frame),
							Right:  held.renderBar(segment.TopRight, frame),
						},
						Input: frame,
						Bottom: input.Ruler{
							Left:   held.renderBar(segment.BottomLeft, frame),
							Center: held.renderBar(segment.BottomCenter, frame),
							Right:  held.renderBar(segment.BottomRight, frame),
						},
					}

					rows, cursorRow, cursorColumn := block.Rows(width)

					return fmt.Sprintf(
						"%s\ncursor row %d column %d",
						strings.Join(rows, "\n"), cursorRow, cursorColumn,
					)
				})
			}

			shownPassesAtWidth[passName] = func() string {
				drawn := passes[passName]()

				return shown(t, drawn[:strings.LastIndex(drawn, "\ncursor row ")], width)
			}
		}
	}

	compareWithGolden(t, "inputblock", ".ansi", passes)
	compareWithGolden(t, "inputblock", ".screen", shownPassesAtWidth)
}

func TestVerticalInputMovementDrawsWhatItDrewBefore(t *testing.T) {
	up := key.Key{Code: key.Up}
	down := key.Key{Code: key.Down}

	passes := map[string]func() string{
		"1 wrapped draft": func() string {
			return verticalInputMovementStream(t)
		},
		"2 up within the draft": func() string {
			return verticalInputMovementStream(t, up)
		},
		"3 up into history": func() string {
			return verticalInputMovementStream(t, up, up)
		},
		"4 down within the draft": func() string {
			return verticalInputMovementStream(t, up, down)
		},
	}

	shownPasses := map[string]func() string{}
	for name, pass := range passes {
		shownPasses[name] = func() string {
			return shown(t, pass(), tinyColumns)
		}
	}

	compareWithGolden(t, "vertical-movement", ".ansi", passes)
	compareWithGolden(t, "vertical-movement", ".screen", shownPasses)
}

func verticalInputMovementStream(t *testing.T, keypresses ...key.Key) string {
	t.Helper()

	self := slashCommandFixture(t, caps.Read)
	var screenOutput strings.Builder
	self.screen = output.NewTerminalOfSize(&screenOutput, tinyColumns, replayLines)

	history := edit.NewHistory("", historyLimit)
	history.Add("earlier")
	editor := edit.NewInput(history)
	editor.SetText("one two three")
	self.show(editor)

	for _, keypress := range keypresses {
		self.handleKeypressAndShowInput(editor, history, keypress)
	}

	return screenOutput.String()
}

func TestReadlineInputBindingsDrawWhatTheyDrewBefore(t *testing.T) {
	control := func(value rune) key.Key {
		return key.Key{Code: key.Rune, Value: value, Mod: key.Ctrl}
	}
	character := func(value rune) key.Key {
		return key.Key{Code: key.Rune, Value: value}
	}

	historyLines := []string{"git status", "just test", "git diff"}
	passes := map[string]func() string{
		"01 ctrl+a": func() string {
			return readlineInputStream(t, nil, "one two", control('a'), character('!'))
		},
		"02 ctrl+e": func() string {
			return readlineInputStream(t, nil, "one two", key.Key{Code: key.Home}, control('e'), character('!'))
		},
		"03 ctrl+b": func() string {
			return readlineInputStream(t, nil, "one two", control('b'), character('!'))
		},
		"04 ctrl+f": func() string {
			return readlineInputStream(t, nil, "one two", key.Key{Code: key.Home}, control('f'), character('!'))
		},
		"05 ctrl+w": func() string {
			return readlineInputStream(t, nil, "one --two", control('w'))
		},
		"06 ctrl+k": func() string {
			return readlineInputStream(t, nil, "one two", key.Key{Code: key.Home}, control('f'), control('k'))
		},
		"07 ctrl+r empty history": func() string {
			return readlineInputStream(t, nil, "unfinished", control('r'))
		},
		"08 ctrl+r query": func() string {
			return readlineInputStream(t, historyLines, "unfinished", control('r'), character('g'), character('i'), character('t'))
		},
		"09 ctrl+r again": func() string {
			return readlineInputStream(t, historyLines, "unfinished", control('r'), character('g'), character('i'), character('t'), control('r'))
		},
		"10 ctrl+r backspace": func() string {
			return readlineInputStream(t, historyLines, "unfinished", control('r'), character('g'), character('i'), character('t'), key.Key{Code: key.Backspace})
		},
		"11 ctrl+r failed query": func() string {
			return readlineInputStream(t, historyLines, "unfinished", control('r'), character('g'), character('i'), character('t'), character('z'))
		},
		"12 ctrl+r erased query": func() string {
			return readlineInputStream(t, historyLines, "unfinished", control('r'), character('g'), key.Key{Code: key.Backspace})
		},
		"13 ctrl+r escape": func() string {
			return readlineInputStream(t, historyLines, "unfinished", control('r'), character('g'), character('i'), character('t'), key.Key{Code: key.Escape})
		},
		"14 ctrl+r then ctrl+a": func() string {
			return readlineInputStream(t, historyLines, "unfinished", control('r'), character('g'), character('i'), character('t'), control('a'), character('!'))
		},
	}

	shownPasses := map[string]func() string{}
	for name, pass := range passes {
		shownPasses[name] = func() string {
			return shown(t, pass(), 40)
		}
	}

	compareWithGolden(t, "readline-bindings", ".ansi", passes)
	compareWithGolden(t, "readline-bindings", ".screen", shownPasses)
}

func readlineInputStream(t *testing.T, historyLines []string, text string, keypresses ...key.Key) string {
	t.Helper()

	self := slashCommandFixture(t, caps.Read)
	var screenOutput strings.Builder
	self.screen = output.NewTerminalOfSize(&screenOutput, 40, replayLines)

	history := edit.NewHistory("", historyLimit)
	for _, line := range historyLines {
		history.Add(line)
	}
	editor := edit.NewInput(history)
	editor.SetText(text)
	self.show(editor)

	for _, keypress := range keypresses {
		self.handleKeypressAndShowInput(editor, history, keypress)
	}

	return screenOutput.String()
}

func writeLiveConfig(t *testing.T, path string, body string) {
	t.Helper()

	contents := fmt.Sprintf("version = %d\n%s", config.Format, undentConfig(body))
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func prepareLiveConfig(t *testing.T, self *App, path string) {
	t.Helper()

	settings, observer, err := config.Observe(path)
	if err != nil {
		t.Fatal(err)
	}
	self.configObserver = observer
	t.Cleanup(observer.Close)
	registry := availableSegments(workspaceMarker, "brave-otter", "gpt-5.6-sol", "high", self)
	live, err := settings.BuildLive(registry)
	if err != nil {
		t.Fatal(err)
	}
	self.continueMessage = live.ContinueMessage
	self.streamingMode = live.StreamingMode
	self.barConfiguration = bar.NewConfiguration(registry, live.SegmentLayout)
}

func settleLiveConfig(t *testing.T, self *App) {
	t.Helper()

	timeout := time.NewTimer(2 * time.Second)
	defer timeout.Stop()
	for {
		select {
		case failure, open := <-self.configObserver.Changes():
			if !open {
				t.Fatal("config watch closed before reporting the change")
			}
			if self.reloadConfig(failure) {
				return
			}
		case <-timeout.C:
			t.Fatal("timed out waiting for a config change")
		}
	}
}

func TestReloadingConfigChangesTheContinueMessage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	writeLiveConfig(t, path, `
		[input]
		continue = "first"
	`)

	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	prepareLiveConfig(t, self, path)

	writeLiveConfig(t, path, `
		[input]
		continue = "second"
	`)
	settleLiveConfig(t, self)
	writeLiveConfig(t, path, `
		[input]
		continue = "carry on from the reloaded config"
	`)
	settleLiveConfig(t, self)

	history := edit.NewHistory("", historyLimit)
	editor := edit.NewInput(history)
	self.apply(editor, history, key.Key{Code: key.Enter})
	self.apply(editor, history, key.Key{Code: key.Enter})

	for report := range self.currentTurn.Events() {
		self.takeTurn(report)
	}
	self.finish()

	hasSentReloadedMessage := false
	for _, event := range self.events {
		if event.Kind == agent.UserMessageEvent && event.Text == "carry on from the reloaded config" {
			hasSentReloadedMessage = true
		}
		if event.Kind == agent.HarnessMessageEvent {
			t.Errorf("successful reload entered conversation history: %+v", event)
		}
	}
	if !hasSentReloadedMessage {
		t.Error("the reloaded message was not sent")
	}
}

func TestReloadingConfigChangesTheStreamingModeForTheNextTurn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	writeLiveConfig(t, path, "[ui]\nstream = \"asap\"\n")

	self := testConversation(t, &bytes.Buffer{})
	prepareLiveConfig(t, self, path)
	if self.streamingMode != output.StreamingModeASAP {
		t.Fatalf("initial streaming mode is %d, want asap", self.streamingMode)
	}

	writeLiveConfig(t, path, "[ui]\nstream = \"paced\"\n")
	settleLiveConfig(t, self)
	if self.streamingMode != output.StreamingModePaced {
		t.Errorf("reloaded streaming mode is %d, want paced", self.streamingMode)
	}
}

type feedbackScenario int

const (
	feedbackCommandError feedbackScenario = iota
	feedbackHelp
	feedbackSuccess
	feedbackClearedByEditing
	feedbackClearedByTurnCompletion
	feedbackStorageWarnings
	feedbackTallAnswer
)

func TestFeedbackDrawsEveryVisibleState(t *testing.T) {
	passes := map[string]func() string{
		"command error":               func() string { return feedbackStream(t, feedbackCommandError) },
		"multiline help":              func() string { return feedbackStream(t, feedbackHelp) },
		"success confirmation":        func() string { return feedbackStream(t, feedbackSuccess) },
		"editing clears feedback":     func() string { return feedbackStream(t, feedbackClearedByEditing) },
		"turn completion clears it":   func() string { return feedbackStream(t, feedbackClearedByTurnCompletion) },
		"combined storage warnings":   func() string { return feedbackStream(t, feedbackStorageWarnings) },
		"tall answer stays untouched": func() string { return feedbackStream(t, feedbackTallAnswer) },
	}

	compareWithGolden(t, "feedback", ".ansi", passes)
	compareWithGolden(t, "feedback", ".screen", shownPasses(t, passes))
	compareWithGolden(t, "feedback", ".txt", map[string]func() string{
		"plain help": func() string { return plainFeedback(t) },
	})
}

func feedbackStream(t *testing.T, scenario feedbackScenario) string {
	t.Helper()

	var screenOutput strings.Builder
	self := slashCommandFixture(t, caps.Read)
	self.agent = agent.New("", quietProvider{}, nil)
	terminalLines := replayLines
	if scenario == feedbackTallAnswer {
		terminalLines = 8
	}
	self.screen = output.NewTerminalOfSize(&screenOutput, replayColumns, terminalLines)
	self.commands = fixtureCommandRegistry(
		t,
		slash.Command{
			Name: "help",
			Run: func(context slash.Context, _ slash.Arguments) error {
				context.Notice("Commands:\n  /conf\n  /copy")
				return nil
			},
		},
		slash.Command{
			Name: "copy",
			Run: func(context slash.Context, _ slash.Arguments) error {
				context.Success("Copied last message to clipboard")
				return nil
			},
		},
	)

	editor := edit.NewInput(nil)
	self.editor = editor

	switch scenario {
	case feedbackCommandError, feedbackClearedByEditing, feedbackClearedByTurnCompletion, feedbackTallAnswer:
		editor.SetText("/unknown")
	case feedbackHelp:
		editor.SetText("/help")
	case feedbackSuccess:
		editor.SetText("/copy")
	case feedbackStorageWarnings:
	}
	self.show(editor)

	switch scenario {
	case feedbackCommandError:
		self.handleCommand("/unknown")
		self.show(editor)
	case feedbackHelp:
		self.handleCommand("/help")
		self.show(editor)
	case feedbackSuccess:
		self.handleCommand("/copy")
		self.show(editor)
	case feedbackClearedByEditing:
		self.handleCommand("/unknown")
		self.show(editor)
		self.handleKeypressAndShowInput(editor, nil, key.Key{Code: key.Rune, Value: 'x'})
	case feedbackClearedByTurnCompletion:
		self.currentTurn = Turn{Stream: testRunningTurnStream(), painter: self.newPainter(true)}
		self.currentTurn.painter.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: "still working"})
		self.handleCommand("/unknown")
		self.show(editor)
		completed := agent.Event{Kind: agent.ModelMessageEvent, Text: "still working"}
		self.takeTurn(TurnEvent{Update: agent.Update{Event: &completed}})
		self.finish()
		self.show(editor)
	case feedbackStorageWarnings:
		self.notifyFailure("chat.md recording disabled: transcript append failed\nwire.http recording disabled: wire append failed")
		self.show(editor)
	case feedbackTallAnswer:
		const answer = "01 alpha\n\n02 bravo\n\n03 charlie\n\n04 delta\n\n05 echo\n\n06 foxtrot\n\n07 golf"
		self.currentTurn = Turn{Stream: testRunningTurnStream(), painter: self.newPainter(true)}
		self.currentTurn.painter.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: answer})
		self.handleCommand("/unknown")
		self.show(editor)
	}

	return screenOutput.String()
}

func plainFeedback(t *testing.T) string {
	t.Helper()

	var screenOutput bytes.Buffer
	self := slashCommandFixture(t, caps.Read)
	self.screen = output.New(&screenOutput)
	self.isPlain = true
	self.commands = fixtureCommandRegistry(t, slash.Command{
		Name: "help",
		Run: func(context slash.Context, _ slash.Arguments) error {
			context.Notice("Commands:\n  /help")
			return nil
		},
	})
	self.handleCommand("/help")
	self.screen.End()
	return strings.TrimSuffix(style.Plain(screenOutput.String()), "\n")
}

type configReloadScenario int

const (
	configReloadValid configReloadScenario = iota
	configReloadInvalidRecovery
	configReloadDeletion
	configReloadWatchFailure
	configReloadReplay
	configReloadSettingThatIsNotLive
)

func TestReloadingConfigDrawsEveryVisibleState(t *testing.T) {
	passes := map[string]func() string{
		"valid revision":                   func() string { return configReloadStream(t, configReloadValid) },
		"invalid revision then recovery":   func() string { return configReloadStream(t, configReloadInvalidRecovery) },
		"deleted config restores defaults": func() string { return configReloadStream(t, configReloadDeletion) },
		"filesystem watch failure":         func() string { return configReloadStream(t, configReloadWatchFailure) },
		"replayed failure and recovery":    func() string { return configReloadStream(t, configReloadReplay) },
		"revision to a setting that only a restart picks up": func() string {
			return configReloadStream(t, configReloadSettingThatIsNotLive)
		},
	}

	compareWithGolden(t, "config-reload", ".ansi", passes)
	compareWithGolden(t, "config-reload", ".screen", shownPasses(t, passes))
}

func TestEphemeralInterfaceFeedbackStaysOutOfConversationHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	writeLiveConfig(t, path, `
		[input]
		continue = "first"
	`)

	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })

	const terminalLines = 8
	const answer = "01 alpha\n\n02 bravo\n\n03 charlie\n\n04 delta\n\n05 echo\n\n06 foxtrot\n\n07 golf"

	var liveOutput bytes.Buffer
	self := &App{
		agent:    agent.New("", quietProvider{}, nil),
		screen:   output.NewTerminalOfSize(&liveOutput, replayColumns, terminalLines),
		recorder: record.New(log),
		mode:     caps.NewMode(caps.Read),
		commands: fixtureCommandRegistry(t, slash.Command{
			Name: "help",
			Run: func(context slash.Context, _ slash.Arguments) error {
				context.Notice("Commands:\n  /help")
				return nil
			},
		}),
	}
	prepareLiveConfig(t, self, path)
	self.currentTurn = Turn{Stream: testRunningTurnStream(), painter: self.newPainter(true)}
	self.recordEvent(agent.Event{Kind: agent.UserMessageEvent, Text: "show me the answer"})
	self.currentTurn.painter.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: answer})

	beforeFeedback := liveOutput.String()
	self.handleCommand("/foo")
	self.handleCommand("/help")
	writeLiveConfig(t, path, `
		[input]
		continue = "second"
	`)
	settleLiveConfig(t, self)
	writeLiveConfig(t, path, `
		[input]
		continue = "third"
	`)
	settleLiveConfig(t, self)

	if liveOutput.String() != beforeFeedback {
		t.Error("ephemeral interface feedback changed conversation scrollback before the answer was sealed")
	}
	if len(self.events) != 1 || self.events[0].Kind != agent.UserMessageEvent {
		t.Errorf("conversation events before the answer was sealed are %v, want [user_message]", getEventKinds(self.events))
	}

	completed := agent.Event{Kind: agent.ModelMessageEvent, Text: answer}
	self.takeTurn(TurnEvent{Update: agent.Update{Event: &completed}})
	self.finish()

	sessionName := log.Name()
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	storedSession, err := store.Read(directory, sessionName)
	if err != nil {
		t.Fatal(err)
	}
	if len(storedSession.Events) != 2 ||
		storedSession.Events[0].Kind != agent.UserMessageEvent ||
		storedSession.Events[1].Kind != agent.ModelMessageEvent {
		t.Errorf("stored conversation events are %v, want [user_message model_message]", getEventKinds(storedSession.Events))
	}

	var replayOutput bytes.Buffer
	replayed := &App{
		agent:  agent.New("", quietProvider{}, nil),
		screen: output.NewTerminalOfSize(&replayOutput, replayColumns, terminalLines),
		events: storedSession.Events,
	}
	replayed.replay()

	liveScreen := visibleScreen(t, liveOutput.String(), replayColumns)
	restoredScreen := visibleScreen(t, replayOutput.String(), replayColumns)
	if !slices.Equal(liveScreen, restoredScreen) {
		t.Errorf(
			"ephemeral interface feedback changed the stored replay\nlive:\n%s\nreplayed:\n%s",
			strings.Join(liveScreen, "\n"),
			strings.Join(restoredScreen, "\n"),
		)
	}
}

func TestRetryRemainsDurableConversationHistory(t *testing.T) {
	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })

	var liveOutput bytes.Buffer
	self := &App{
		agent:    agent.New("", quietProvider{}, nil),
		screen:   output.NewTerminalOfSize(&liveOutput, replayColumns, replayLines),
		recorder: record.New(log),
		mode:     caps.NewMode(caps.Read),
	}
	self.currentTurn = Turn{Stream: testRunningTurnStream(), painter: self.newPainter(true)}
	self.recordEvent(agent.Event{Kind: agent.UserMessageEvent, Text: "try it"})
	self.recordEvent(agent.Event{
		Kind:    agent.RetryingEvent,
		Text:    "temporary fault",
		Attempt: 1,
		Took:    300 * time.Millisecond,
	})
	self.recordEvent(agent.Event{Kind: agent.ModelMessageEvent, Text: "done"})
	self.finish()

	sessionName := log.Name()
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	storedSession, err := store.Read(directory, sessionName)
	if err != nil {
		t.Fatal(err)
	}
	if len(storedSession.Events) != 3 ||
		storedSession.Events[0].Kind != agent.UserMessageEvent ||
		storedSession.Events[1].Kind != agent.RetryingEvent ||
		storedSession.Events[2].Kind != agent.ModelMessageEvent {
		t.Errorf("stored conversation events are %v, want [user_message retrying model_message]", getEventKinds(storedSession.Events))
	}

	var replayOutput bytes.Buffer
	replayed := &App{
		agent:  agent.New("", quietProvider{}, nil),
		screen: output.NewTerminalOfSize(&replayOutput, replayColumns, replayLines),
		events: storedSession.Events,
	}
	replayed.replay()

	if !strings.Contains(style.Plain(replayOutput.String()), "Request failed; retrying in 0.3s: Temporary fault") {
		t.Errorf("stored replay omitted the retry:\n%s", style.Plain(replayOutput.String()))
	}
	requireSameVisibleScreen(t, "durable retry changed after replay", liveOutput.String(), replayOutput.String())
}

func getEventKinds(events []agent.Event) []agent.Kind {
	kinds := make([]agent.Kind, len(events))
	for i, event := range events {
		kinds[i] = event.Kind
	}
	return kinds
}

func configReloadStream(t *testing.T, scenario configReloadScenario) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.toml")
	writeLiveConfig(t, path, `
		[input]
		continue = "first"

		[bar.top]
		left = [{ segment = "session-name" }]
		center = []
		right = []

		[bar.bottom]
		left = []
		center = []
		right = []
	`)

	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	self.screen = output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines)
	prepareLiveConfig(t, self, path)
	editor := edit.NewInput(nil)
	self.show(editor)

	switch scenario {
	case configReloadDeletion:
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		settleLiveConfig(t, self)
		self.show(editor)
		return screenOutput.String()
	case configReloadWatchFailure:
		self.reloadConfig(errors.New("inotify stopped"))
		self.show(editor)
		return screenOutput.String()
	case configReloadSettingThatIsNotLive:
		writeLiveConfig(t, path, `
			[input]
			continue = "first"

			[editor]
			command = "vi"

			[bar.top]
			left = [{ segment = "session-name" }]
			center = []
			right = []

			[bar.bottom]
			left = []
			center = []
			right = []
		`)
		settleLiveConfig(t, self)
		self.show(editor)
		return screenOutput.String()
	case configReloadInvalidRecovery, configReloadReplay:
		writeLiveConfig(t, path, `
			[bar.top]
			left = [{ segment = "missing" }]
		`)
		settleLiveConfig(t, self)
	case configReloadValid:
	}

	writeLiveConfig(t, path, `
		[input]
		continue = "second"

		[bar.top]
		left = []
		center = []
		right = []

		[bar.bottom]
		left = []
		center = [{ segment = "active-model" }]
		right = []
	`)
	settleLiveConfig(t, self)
	self.show(editor)
	if scenario != configReloadReplay {
		return screenOutput.String()
	}

	var replayOutput bytes.Buffer
	replayed := testConversation(t, &replayOutput)
	replayed.screen = output.NewTerminalOfSize(&replayOutput, replayColumns, replayLines)
	prepareLiveConfig(t, replayed, path)
	replayed.events = append(replayed.events, self.events...)
	replayed.replay()
	replayed.show(edit.NewInput(nil))

	liveScreen := visibleScreen(t, screenOutput.String(), replayColumns)
	restoredScreen := visibleScreen(t, replayOutput.String(), replayColumns)
	if !slices.Equal(liveScreen, restoredScreen) {
		t.Errorf(
			"reloaded config changed after replay\nlive:\n%s\nreplayed:\n%s",
			strings.Join(liveScreen, "\n"),
			strings.Join(restoredScreen, "\n"),
		)
	}
	return replayOutput.String()
}

type screen struct {
	t           *testing.T
	rows        [][]string
	row, column int
	columns     int
	isWrapping  bool
}

func visibleScreen(t *testing.T, stream string, columns int) []string {
	t.Helper()

	self := &screen{t: t, columns: columns, isWrapping: true}
	self.play(stream)

	return self.text()
}

func (self *screen) play(stream string) {
	for at := 0; at < len(stream); {
		switch stream[at] {
		case '\x1b':
			at = self.escape(stream, at)
		case '\r':
			self.column = 0
			at++
		case '\n':
			self.row++
			self.column = 0
			at++
		default:
			end := at + 1
			for end < len(stream) && stream[end] != '\x1b' && stream[end] != '\r' && stream[end] != '\n' {
				end++
			}

			for grapheme, cells := range width.Graphemes(stream[at:end]) {
				self.put(grapheme, cells)
			}
			at = end
		}
	}
}

func (self *screen) escape(stream string, at int) int {
	if at+1 >= len(stream) {
		return len(stream)
	}

	switch stream[at+1] {
	case '[':
		return self.control(stream, at)
	case ']':
		return self.operatingSystemCommand(stream, at)
	default:
		self.t.Fatalf("the screen was sent an escape it does not know: %q", stream[at:min(at+8, len(stream))])
		return len(stream)
	}
}

func (self *screen) operatingSystemCommand(stream string, at int) int {
	end := skipUntilStringTerminator(stream, at)
	terminatorWidth := 1
	if end >= 2 && stream[end-2:end] == "\x1b\\" {
		terminatorWidth = 2
	}
	payload := stream[at+2 : end-terminatorWidth]
	if !strings.HasPrefix(payload, "66;") {
		return end
	}

	parts := strings.SplitN(payload, ";", 3)
	if len(parts) != 3 {
		return end
	}

	scale := 1
	declaredWidth := width.Of(parts[2])
	for option := range strings.SplitSeq(parts[1], ":") {
		scale = prefixedNumber(option, "s=", scale)
		declaredWidth = prefixedNumber(option, "w=", declaredWidth)
	}
	self.put(parts[2], scale*declaredWidth)

	return end
}

func (self *screen) control(stream string, at int) int {
	end := at + 2
	for end < len(stream) && (stream[end] < 0x40 || stream[end] > 0x7e) {
		end++
	}

	if end >= len(stream) {
		return len(stream)
	}

	parameters := stream[at+2 : end]
	self.apply(stream[end], parameters)

	return end + 1
}

func (self *screen) apply(command byte, parameters string) {
	if strings.HasPrefix(parameters, "?") {
		self.privateMode(command, parameters)
		return
	}

	count := max(1, numberOr(parameters, 1))

	switch command {
	case 'm':
	case 'A':
		self.row = max(0, self.row-count)
	case 'B':
		self.row += count
	case 'C':
		self.column += count
	case 'D':
		self.column = max(0, self.column-count)
	case 'H':
		self.row, self.column = 0, 0
	case 'K':
		self.eraseInRow(numberOr(parameters, 0))
	case 'J':
		self.erase(numberOr(parameters, 0))
	default:
		self.t.Fatalf("the screen was sent a control it does not know: ESC [ %s%c", parameters, command)
	}
}

func (self *screen) privateMode(command byte, parameters string) {
	if command != 'h' && command != 'l' {
		self.t.Fatalf("the screen was sent a private mode it does not know: ESC [ %s%c", parameters, command)
	}

	switch parameters {
	case "?7":
		self.isWrapping = command == 'h'
	case "?25", "?2026":
	default:
		self.t.Fatalf("the screen was sent a private mode it does not know: ESC [ %s%c", parameters, command)
	}
}

func (self *screen) eraseInRow(mode int) {
	if self.row >= len(self.rows) {
		return
	}

	row := self.rows[self.row]

	switch mode {
	case 0:
		if self.column < len(row) {
			self.rows[self.row] = row[:self.column]
		}
	case 1:
		for at := range min(self.column+1, len(row)) {
			row[at] = " "
		}
	case 2:
		self.rows[self.row] = nil
	}
}

func (self *screen) erase(mode int) {
	switch mode {
	case 0:
		self.eraseInRow(0)

		if self.row+1 < len(self.rows) {
			self.rows = self.rows[:self.row+1]
		}
	case 2, 3:
		self.rows = nil
	default:
		self.t.Fatalf("the screen was asked to erase in a way it does not know: ESC [ %dJ", mode)
	}
}

func (self *screen) put(grapheme string, cells int) {
	if cells == 0 {
		for len(self.rows) <= self.row {
			self.rows = append(self.rows, nil)
		}
		if self.column > 0 {
			self.rows[self.row][self.column-1] += grapheme
		}
		return
	}

	if self.column+cells > self.columns && self.column > 0 {
		if !self.isWrapping {
			return
		}

		self.row++
		self.column = 0
	}

	for len(self.rows) <= self.row {
		self.rows = append(self.rows, nil)
	}

	drawnCells := min(cells, self.columns-self.column)
	row := self.rows[self.row]
	for len(row) < self.column+drawnCells {
		row = append(row, " ")
	}

	row[self.column] = grapheme
	for i := 1; i < drawnCells; i++ {
		row[self.column+i] = ""
	}
	self.rows[self.row] = row
	self.column = min(self.column+cells, self.columns)
}

func (self *screen) text() []string {
	lines := make([]string, 0, len(self.rows))

	for _, row := range self.rows {
		lines = append(lines, strings.TrimRight(strings.Join(row, ""), " "))
	}

	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	return lines
}

func skipUntilStringTerminator(stream string, at int) int {
	for end := at + 2; end < len(stream); end++ {
		switch {
		case stream[end] == '\a':
			return end + 1
		case stream[end] == '\x1b' && end+1 < len(stream) && stream[end+1] == '\\':
			return end + 2
		}
	}

	return len(stream)
}

func prefixedNumber(parameter string, prefix string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimPrefix(parameter, prefix))
	if err != nil || !strings.HasPrefix(parameter, prefix) {
		return fallback
	}

	return value
}

func numberOr(parameters string, fallback int) int {
	if parameters == "" {
		return fallback
	}

	value, err := strconv.Atoi(strings.Split(parameters, ";")[0])
	if err != nil {
		return fallback
	}

	return value
}

type goldenSegmentOptions string

func (self goldenSegmentOptions) Read(into any) error {
	_, err := toml.Decode(string(self), into)

	return err
}

func goldenSegmentPass(
	t *testing.T,
	factory segment.Factory,
	options string,
	context segment.Context,
) func() string {
	t.Helper()

	built, err := factory(goldenSegmentOptions(options))
	if err != nil {
		t.Fatal(err)
	}

	return func() string { return built.Render(context) }
}

func goldenRepository(t *testing.T, head string) string {
	t.Helper()

	workspaceDir := t.TempDir()
	gitDir := filepath.Join(workspaceDir, ".git")

	if err := os.MkdirAll(gitDir, 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte(head), 0o600); err != nil {
		t.Fatal(err)
	}

	return workspaceDir
}

func availableSegments(
	workspaceDir string,
	currentSessionName string,
	modelName string,
	modelEffort string,
	harness *App,
) segment.Registry {
	return bar.NewRegistry(bar.Options{
		WorkspaceDir:       workspaceDir,
		CurrentSessionName: currentSessionName,
		ModelName:          modelName,
		ModelEffort:        modelEffort,
		ModelEffortLevels:  []string{"none", "minimal", "low", "medium", "high"},
		Sources:            harness.getBarSources(),
	})
}

func renderBar(layout segment.Layout, position segment.Position) string {
	return bar.Render(layout, position, segment.Context{})
}

func goldenBarLayout(t *testing.T, harness *App) segment.Layout {
	t.Helper()

	config := configFrom(t, `
		[bar.top]
		left = []
		center = []
		right = [{ segment = "scroll-overflow", direction = "up" }]

		[bar.bottom]
		left = [
			{ segment = "activity-spinner", idle = "✧··", frames = ["✦··", "·✦·", "··✦", "··✧", "·✧·", "✧··"], rate = "125ms" },
			{ segment = "turn-timer" },
			{ segment = "mode-toggle" },
			{ segment = "workspace-dir" },
			{ segment = "git-branch" },
			{ segment = "active-model" },
			{ segment = "context-usage" },
		]
		center = []
		right = [
			{ segment = "turn-count" },
			{ segment = "session-name" },
			{ segment = "local-time", format = "15:04" },
			{ segment = "scroll-overflow", direction = "down" },
		]
	`)

	layout, err := config.BuildLayout(
		availableSegments(workspaceMarker, "brave-otter", "gpt-5.6-sol", "high", harness),
	)
	if err != nil {
		t.Fatal(err)
	}

	return layout
}

func clockAt(at time.Time) func() time.Time {
	return func() time.Time { return at }
}

func goldenSchedulePass(t *testing.T, isRunning bool, workPerPass time.Duration, span time.Duration) func() string {
	t.Helper()

	return func() string {
		return drawnOnAStoppedClock(t, func(t *testing.T) string {
			t.Helper()

			startedAt := time.Now()

			held := &App{mode: caps.NewMode(caps.All())}
			held.currentTurn.Stream = testTimedTurnStream(isRunning, startedAt, startedAt)
			held.barConfiguration = bar.NewConfiguration(nil, goldenBarLayout(t, held))

			return filmstrip(startedAt, span, workPerPass, func() time.Time {
				return held.nextBarRefresh(time.Now())
			}, func() string {
				return held.barConfiguration.Render(segment.BottomLeft, segment.Context{})
			})
		})
	}
}

func goldenTimerSchedulePass(t *testing.T, workPerPass time.Duration, span time.Duration) func() string {
	t.Helper()

	return func() string {
		return drawnOnAStoppedClock(t, func(t *testing.T) string {
			t.Helper()

			startedAt := time.Now()

			held := &App{mode: caps.NewMode(caps.All())}
			held.currentTurn.Stream = testTimedTurnStream(true, startedAt, time.Time{})

			built, err := turnTimer.New(held.turnTiming, held.isTurnRunning)(goldenSegmentOptions(""))
			if err != nil {
				t.Fatal(err)
			}

			layout := segment.Layout{segment.BottomLeft: {built}}

			return filmstrip(startedAt, span, workPerPass, func() time.Time {
				return layout.NextRefresh(segment.Phase{At: time.Now(), IsRunning: true})
			}, func() string {
				return renderBar(layout, segment.BottomLeft)
			})
		})
	}
}

func goldenClockSchedulePass(t *testing.T, format string, span time.Duration) func() string {
	t.Helper()

	return func() string {
		return drawnOnAStoppedClock(t, func(t *testing.T) string {
			t.Helper()

			startedAt := time.Now()

			built, err := localTime.New(time.Now)(goldenSegmentOptions(fmt.Sprintf("format = %q", format)))
			if err != nil {
				t.Fatal(err)
			}

			layout := segment.Layout{segment.TopRight: {built}}

			return filmstrip(startedAt, span, 0, func() time.Time {
				return layout.NextRefresh(segment.Phase{At: time.Now()})
			}, func() string {
				return renderBar(layout, segment.TopRight)
			})
		})
	}
}

func filmstrip(
	startedAt time.Time,
	span time.Duration,
	workPerPass time.Duration,
	getNextRefresh func() time.Time,
	draw func() string,
) string {
	var strip strings.Builder

	for {
		at := getNextRefresh()
		if at.IsZero() || at.Sub(startedAt) > span {
			return strip.String()
		}

		time.Sleep(time.Until(at))
		since := "+" + time.Since(startedAt).Truncate(time.Millisecond).String()
		fmt.Fprintf(&strip, "%9s  %s\n", since, draw())
		time.Sleep(workPerPass)
	}
}

func TestTheBarsRedrawScheduleRunsWhenItRanBefore(t *testing.T) {
	t.Setenv("HOME", "/home/tester")

	passes := map[string]func() string{
		"running turn":                      goldenSchedulePass(t, true, 0, 2*time.Second),
		"running turn with a slow pass":     goldenSchedulePass(t, true, 40*time.Millisecond, 2*time.Second),
		"running turn with a pass past due": goldenSchedulePass(t, true, 200*time.Millisecond, 2*time.Second),
		"waiting between turns":             goldenSchedulePass(t, false, 0, 3*time.Second),
		"turn timer alone on a slow loop":   goldenTimerSchedulePass(t, 200*time.Millisecond, 3*time.Minute),
		"clock alone to the minute":         goldenClockSchedulePass(t, "15:04", 3*time.Minute),
		"clock alone to the second":         goldenClockSchedulePass(t, "15:04:05", 3*time.Second),
	}

	compareWithGolden(t, "schedule", ".ansi", passes)
	compareWithGolden(t, "schedule", ".screen", shownPasses(t, passes))
}

func TestEverySegmentDrawsItsRepresentativeStates(t *testing.T) {
	t.Setenv("HOME", "/home/tester")

	at := time.Date(2026, time.August, 23, 14, 32, 9, 0, time.UTC)
	spinnerOptions := `
		idle = "✧·"
		frames = ["✦·", "·✦"]
		rate = "125ms"
	`

	passes := map[string]func() string{
		"activity-spinner / idle": goldenSegmentPass(
			t,
			activitySpinner.New(func() bool { return false }, clockAt(at)),
			spinnerOptions,
			segment.Context{},
		),
		"activity-spinner / running first frame": goldenSegmentPass(
			t,
			activitySpinner.New(func() bool { return true }, clockAt(at)),
			spinnerOptions,
			segment.Context{},
		),
		"activity-spinner / running second frame": goldenSegmentPass(
			t,
			activitySpinner.New(func() bool { return true }, clockAt(at.Add(125*time.Millisecond))),
			spinnerOptions,
			segment.Context{},
		),
		"activity-spinner / running frames coming round again": goldenSegmentPass(
			t,
			activitySpinner.New(func() bool { return true }, clockAt(at.Add(250*time.Millisecond))),
			spinnerOptions,
			segment.Context{},
		),
		"activity-spinner / running part way through a frame": goldenSegmentPass(
			t,
			activitySpinner.New(func() bool { return true }, clockAt(at.Add(190*time.Millisecond))),
			spinnerOptions,
			segment.Context{},
		),
		"context-usage / known": goldenSegmentPass(
			t,
			contextUsage.New(func() (int, int) { return 182_000, 200_000 }),
			"",
			segment.Context{},
		),
		"context-usage / one million": goldenSegmentPass(
			t,
			contextUsage.New(func() (int, int) { return 500_000, 1_000_000 }),
			"",
			segment.Context{},
		),
		"context-usage / unknown": goldenSegmentPass(
			t,
			contextUsage.New(func() (int, int) { return 0, 0 }),
			"",
			segment.Context{},
		),
		"git-branch / outside a repository": goldenSegmentPass(
			t,
			gitBranch.New(workspaceMarker),
			"",
			segment.Context{},
		),
		"git-branch / on a branch": goldenSegmentPass(
			t,
			gitBranch.New(goldenRepository(t, "ref: refs/heads/feature/bars\n")),
			"",
			segment.Context{},
		),
		"git-branch / detached head": goldenSegmentPass(
			t,
			gitBranch.New(goldenRepository(t, "1fd19004e0f4a2c8b4c5d6e7f8a9b0c1d2e3f4a5\n")),
			"",
			segment.Context{},
		),
		"turn-count / nothing asked yet": goldenSegmentPass(
			t,
			turnCount.New(func() int { return 0 }),
			"",
			segment.Context{},
		),
		"turn-count / third turn": goldenSegmentPass(
			t,
			turnCount.New(func() int { return 3 }),
			"",
			segment.Context{},
		),
		"session-name": goldenSegmentPass(
			t,
			sessionName.New("brave-otter"),
			"",
			segment.Context{},
		),
		"session-name / emoji": goldenSegmentPass(
			t,
			sessionName.New("brave-otter"),
			"emoji = true",
			segment.Context{},
		),
		"session-emoji": goldenSegmentPass(
			t,
			sessionEmoji.New("brave-otter"),
			"",
			segment.Context{},
		),
		"session-emoji / unknown animal": goldenSegmentPass(
			t,
			sessionEmoji.New("brave-tester"),
			"",
			segment.Context{},
		),
		"session-emoji / retired animal": goldenSegmentPass(
			t,
			sessionEmoji.New(retiredAnimalSession),
			"",
			segment.Context{},
		),
		"session-name / retired animal": goldenSegmentPass(
			t,
			sessionName.New(retiredAnimalSession),
			"emoji = true",
			segment.Context{},
		),
		"local-time / custom format": goldenSegmentPass(
			t,
			localTime.New(func() time.Time { return at }),
			`format = "15:04:05"`,
			segment.Context{},
		),
		"local-time / default format": goldenSegmentPass(
			t,
			localTime.New(func() time.Time { return at }),
			"",
			segment.Context{},
		),
		"mode-toggle / all granted": goldenSegmentPass(
			t,
			modeToggle.New(caps.All, func() bool { return false }),
			"",
			segment.Context{},
		),
		"mode-toggle / pending prefix": goldenSegmentPass(
			t,
			modeToggle.New(func() caps.Set { return caps.Read }, func() bool { return true }),
			"",
			segment.Context{},
		),
		"mode-toggle / read only": goldenSegmentPass(
			t,
			modeToggle.New(func() caps.Set { return caps.Read }, func() bool { return false }),
			"",
			segment.Context{},
		),
		"mode-toggle / web only": goldenSegmentPass(
			t,
			modeToggle.New(func() caps.Set { return caps.Read | caps.Web }, func() bool { return false }),
			"",
			segment.Context{},
		),
		"scroll-overflow / down": goldenSegmentPass(
			t,
			scrollOverflow.New,
			`direction = "down"`,
			segment.Context{HiddenLinesBelow: 7},
		),
		"scroll-overflow / empty": goldenSegmentPass(
			t,
			scrollOverflow.New,
			`direction = "up"`,
			segment.Context{},
		),
		"scroll-overflow / up": goldenSegmentPass(
			t,
			scrollOverflow.New,
			`direction = "up"`,
			segment.Context{HiddenLinesAbove: 3},
		),
		"subscription-usage / not applicable": goldenSegmentPass(
			t,
			subUsage.New(nil, "", "", func() time.Time { return at }),
			"",
			segment.Context{},
		),
		"subscription-usage / even burn": goldenUsagePass(t, at, "gpt-5.6-sol", usageReport{
			windows: []agent.UsageWindow{
				{Duration: 5 * time.Hour, Percent: 40, ResetsAt: at.Add(150 * time.Minute)},
				{Duration: 7 * 24 * time.Hour, Percent: 12, ResetsAt: at.Add(6 * 24 * time.Hour)},
			},
		}),
		"subscription-usage / ahead of pace": goldenUsagePass(t, at, "gpt-5.6-sol", usageReport{
			windows: []agent.UsageWindow{
				{Duration: 5 * time.Hour, Percent: 28, ResetsAt: at.Add(4 * time.Hour)},
			},
		}),
		"subscription-usage / near the limit": goldenUsagePass(t, at, "gpt-5.6-sol", usageReport{
			windows: []agent.UsageWindow{
				{Duration: 5 * time.Hour, Percent: 95, ResetsAt: at.Add(4 * time.Hour)},
			},
		}),
		"subscription-usage / spent": goldenUsagePass(t, at, "gpt-5.6-sol", usageReport{
			windows: []agent.UsageWindow{
				{
					Duration:  5 * time.Hour,
					Percent:   100,
					ResetsAt:  at.Add(time.Hour),
					IsLimited: true,
				},
			},
		}),
		"subscription-usage / window already elapsed": goldenUsagePass(t, at, "gpt-5.6-sol", usageReport{
			windows: []agent.UsageWindow{
				{Duration: 5 * time.Hour, Percent: 68, ResetsAt: at.Add(-time.Minute)},
			},
		}),
		"subscription-usage / no reset reported": goldenUsagePass(t, at, "gpt-5.6-sol", usageReport{
			windows: []agent.UsageWindow{
				{Duration: 7 * 24 * time.Hour, Percent: 6},
			},
		}),
		"subscription-usage / weekly plan window": goldenUsagePass(t, at, "gpt-5.6-sol", usageReport{
			windows: []agent.UsageWindow{
				{Duration: 7 * 24 * time.Hour, Percent: 6, ResetsAt: at.Add(6 * 24 * time.Hour)},
			},
		}),
		"subscription-usage / metered model in use": goldenUsagePass(
			t,
			at,
			"gpt-5.3-codex-spark",
			usageReport{windows: meteredWindows(at)},
		),
		"subscription-usage / metered model not in use": goldenUsagePass(
			t,
			at,
			"gpt-5.6-sol",
			usageReport{windows: meteredWindows(at)},
		),
		"subscription-usage / only other models metered": goldenUsagePass(
			t,
			at,
			"claude-sonnet-4-6",
			usageReport{windows: []agent.UsageWindow{
				{
					Duration: 7 * 24 * time.Hour,
					Percent:  3,
					ResetsAt: at.Add(6 * 24 * time.Hour),
					Scope:    "opus",
				},
			}},
		),
		"subscription-usage / three windows": goldenUsagePass(t, at, "kimi-k3", usageReport{
			windows: []agent.UsageWindow{
				{Duration: 5 * time.Hour, Percent: 39, ResetsAt: at.Add(4 * time.Hour)},
				{Duration: 7 * 24 * time.Hour, Percent: 15, ResetsAt: at.Add(6 * 24 * time.Hour)},
				{Duration: 30 * 24 * time.Hour, Percent: 13, ResetsAt: at.Add(20 * 24 * time.Hour)},
			},
		}),
		"subscription-usage / refused": goldenUsagePass(t, at, "gpt-5.6-sol", usageReport{
			err: &req.StatusError{Status: 401, Message: "the key is not yours"},
		}),
		"subscription-usage / unreachable": goldenUsagePass(t, at, "gpt-5.6-sol", usageReport{
			err: errors.New("the endpoint is sulking"),
		}),
		"subscription-usage / nothing reported yet": goldenUsagePass(
			t,
			at,
			"gpt-5.6-sol",
			usageReport{},
		),
		"subscription-usage / another session's snapshot": goldenUsageFromCache(
			t,
			at,
			"gpt-5.6-sol",
			[]agent.UsageWindow{
				{Duration: 5 * time.Hour, Percent: 40, ResetsAt: at.Add(150 * time.Minute)},
				{Duration: 7 * 24 * time.Hour, Percent: 12, ResetsAt: at.Add(6 * 24 * time.Hour)},
			},
		),
		"subscription-usage / refreshing keeps the figures": goldenUsagePass(
			t,
			at,
			"gpt-5.6-sol",
			usageReport{
				windows: []agent.UsageWindow{
					{Duration: 5 * time.Hour, Percent: 40, ResetsAt: at.Add(150 * time.Minute)},
				},
				thenErr: &req.StatusError{Status: 429, Message: "slow down"},
			},
		),
		"turn-timer / user turn": goldenSegmentPass(
			t,
			turnTimer.New(func() turn.Timing {
				return turn.Timing{UserTurn: 12 * time.Minute, ModelTurn: 3 * time.Minute}
			}, func() bool {
				return false
			}),
			"",
			segment.Context{},
		),
		"turn-timer / model turn": goldenSegmentPass(
			t,
			turnTimer.New(func() turn.Timing {
				return turn.Timing{UserTurn: 3 * time.Minute, ModelTurn: time.Minute}
			}, func() bool {
				return true
			}),
			"",
			segment.Context{},
		),
		"turn-timer / nothing asked yet": goldenSegmentPass(
			t,
			turnTimer.New(func() turn.Timing { return turn.Timing{} }, func() bool { return false }),
			"",
			segment.Context{},
		),
		"turn-timer / part of a minute": goldenSegmentPass(
			t,
			turnTimer.New(func() turn.Timing {
				return turn.Timing{UserTurn: 40 * time.Second, ModelTurn: 20 * time.Second}
			}, func() bool {
				return true
			}),
			"",
			segment.Context{},
		),
		"turn-timer / turns past an hour": goldenSegmentPass(
			t,
			turnTimer.New(func() turn.Timing {
				return turn.Timing{UserTurn: 3*time.Hour + 5*time.Minute, ModelTurn: 95 * time.Minute}
			}, func() bool {
				return true
			}),
			"",
			segment.Context{},
		),
		"workspace-dir": goldenSegmentPass(
			t,
			workspaceDir.New("/workspace/project"),
			"",
			segment.Context{},
		),
		"workspace-dir / full": goldenSegmentPass(
			t,
			workspaceDir.New("/workspace/project"),
			`type = "full"`,
			segment.Context{},
		),
		"workspace-dir / short below home": goldenSegmentPass(
			t,
			workspaceDir.New("/home/tester/proj/project"),
			`type = "short"`,
			segment.Context{},
		),
		"workspace-dir / short outside home": goldenSegmentPass(
			t,
			workspaceDir.New("/workspace/project"),
			`type = "short"`,
			segment.Context{},
		),
	}

	modelEffortLevels := []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}
	for _, effort := range modelEffortLevels {
		passes["active-model / "+effort+" effort"] = goldenSegmentPass(
			t,
			activeModel.New("gpt-5.6-sol", effort, modelEffortLevels),
			"",
			segment.Context{},
		)
	}
	effortLadders := []struct {
		name   string
		model  string
		effort string
		levels []string
	}{
		{name: "deepseek high", model: "deepseek-v4-pro", effort: "high", levels: []string{"high", "max"}},
		{name: "deepseek max", model: "deepseek-v4-pro", effort: "max", levels: []string{"high", "max"}},
		{name: "opus medium", model: "claude-opus-5", effort: "medium", levels: []string{"low", "medium", "high", "xhigh", "max"}},
		{name: "ollama thinking off", model: "qwen3.8:27b", effort: "none", levels: []string{"none", "low", "medium", "high"}},
		{name: "ollama without thinking", model: "llama3.3:70b", effort: "none", levels: []string{"none"}},
		{name: "sparse ladder", model: "gpt-5.6-sol", effort: "high", levels: []string{"none", "high"}},
	}
	for _, ladder := range effortLadders {
		passes["active-model / "+ladder.name] = goldenSegmentPass(
			t,
			activeModel.New(ladder.model, ladder.effort, ladder.levels),
			"",
			segment.Context{},
		)
	}

	compareWithGolden(t, "segments", ".ansi", passes)
	compareWithGolden(t, "segments", ".screen", shownPasses(t, passes))
}

func writeStoredJournal(t *testing.T, directory string, name string, started string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Join(directory, name), 0o700); err != nil {
		t.Fatal(err)
	}

	head := fmt.Sprintf(`{"kind":"head","time":%q,"id":%q,"name":%q}`+"\n", started, name, name)
	if err := os.WriteFile(filepath.Join(directory, name, "session.jsonl"), []byte(head), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeStoredSession(t *testing.T, directory string, workspaceDir string, name string, started string) {
	t.Helper()

	writeStoredJournal(t, directory, name, started)

	meta := fmt.Sprintf(
		`{"version":%d,"name":%q,"data":{"workspaceDir":%q},"started":%q,"touched":%q}`+"\n",
		session.MetaFormat, name, workspaceDir, started, started,
	)
	if err := os.WriteFile(filepath.Join(directory, name, "meta.json"), []byte(meta), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSessionsComeFromJournalParsing(t *testing.T) {
	directory := t.TempDir()
	meta := json.RawMessage(`{"workspaceDir":"/home/alice/project","model":"gpt"}`)
	writer, err := session.Create(directory, meta, meta)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	sessions, err := sessions.Load(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected one session, got %d", len(sessions))
	}
	if sessions[0].Name != writer.Name() || sessions[0].WorkspaceDir != "/home/alice/project" {
		t.Errorf("unexpected session: %+v", sessions[0])
	}
	if sessions[0].Title != "hello" {
		t.Errorf("expected the provisional title, got %q", sessions[0].Title)
	}
	if sessions[0].Started.IsZero() {
		t.Error("expected the session start time")
	}
}

func TestChoosingWithoutStoredSessionsFails(t *testing.T) {
	if _, err := sessions.Choose(t.TempDir(), t.TempDir(), nil, nil); err == nil {
		t.Error("expected an empty session list to fail")
	}
}

func TestChoosingOnlyOffersTheSessionsOfTheCurrentWorkspace(t *testing.T) {
	directory := t.TempDir()
	workspaceDir := t.TempDir()

	for _, sessionWorkspaceDir := range []string{workspaceDir, t.TempDir()} {
		meta := fmt.Appendf(nil, `{"workspaceDir":%q}`, sessionWorkspaceDir)
		writer, err := session.Create(directory, meta, meta)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "hello"}); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
	}

	loadedSessions, err := sessions.Load(directory)
	if err != nil {
		t.Fatal(err)
	}

	chosen := sessions.InWorkspace(loadedSessions, workspaceDir)
	if len(chosen) != 1 || chosen[0].WorkspaceDir != workspaceDir {
		t.Fatalf("expected only the session of this workspace, got %+v", chosen)
	}

	if _, err := sessions.Choose(directory, t.TempDir(), nil, nil); err == nil {
		t.Error("expected a workspace without sessions to fail")
	}
}

func TestChoosingASessionFromANewerOhAdvisesAnUpgrade(t *testing.T) {
	directory := t.TempDir()
	name := "able-dolphin"

	if err := os.MkdirAll(filepath.Join(directory, name), 0o700); err != nil {
		t.Fatal(err)
	}

	head := fmt.Sprintf(
		`{"kind":"head","time":"2026-08-01T00:00:00Z","version":%d,"id":%q,"name":%q}`+"\n",
		session.JournalFormat+1, name, name,
	)
	if err := os.WriteFile(filepath.Join(directory, name, "session.jsonl"), []byte(head), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := sessions.Choose(directory, t.TempDir(), nil, nil)
	if err == nil {
		t.Fatal("expected the newer session to be refused")
	}
	if !strings.Contains(err.Error(), "upgrade oh") {
		t.Errorf("expected an upgrade to be advised, got %v", err)
	}
	if strings.Contains(err.Error(), "ohctl migrate") {
		t.Errorf("expected migration not to be advised for a newer format, got %v", err)
	}
}

func TestChoosingAnOutdatedSessionAdvisesMigration(t *testing.T) {
	directory := t.TempDir()
	writeStoredJournal(t, directory, "able-dolphin", "2026-08-01T00:00:00Z")

	_, err := sessions.Choose(directory, t.TempDir(), nil, nil)
	if err == nil {
		t.Fatal("expected the outdated session to be refused")
	}
	if !strings.Contains(err.Error(), "run `ohctl migrate`") {
		t.Errorf("expected migration advice, got %v", err)
	}
	if strings.Contains(err.Error(), "meta.json") {
		t.Errorf("expected the missing metadata implementation detail to be hidden, got %v", err)
	}
}

func TestSlashCommandRunsImmediately(t *testing.T) {
	ran := false
	fixtureCommands := fixtureCommandRegistry(t, slash.Command{
		Name: "fixture",
		Run: func(_ slash.Context, arguments slash.Arguments) error {
			if !slices.Equal(arguments.Fields, []string{"one", "two"}) {
				t.Errorf("got arguments %v", arguments.Fields)
			}
			ran = true
			return nil
		},
	})

	self := slashCommandFixture(t, caps.Read|caps.Shell)
	self.commands = fixtureCommands
	if got := self.handleCommand("/fixture one two"); got != dispatch.Handled {
		t.Fatalf("got slash input result %d", got)
	}
	if !ran {
		t.Error("expected the command to run immediately")
	}
}

func TestSlashCommandCanAddFeedback(t *testing.T) {
	fixtureCommands := fixtureCommandRegistry(t, slash.Command{
		Name: "fixture",
		Run: func(context slash.Context, _ slash.Arguments) error {
			context.Notice("fixture notice")
			return nil
		},
	})

	self := slashCommandFixture(t, caps.Read)
	self.screen = output.New(&bytes.Buffer{})
	self.commands = fixtureCommands
	if got := self.handleCommand("/fixture"); got != dispatch.Handled {
		t.Fatalf("got slash input result %d", got)
	}
	if len(self.events) != 0 {
		t.Fatalf("command notice entered conversation events: %v", self.events)
	}
	if self.feedback.message.text != "fixture notice" || self.feedback.message.status != agent.InfoStatus {
		t.Errorf("got feedback %v", self.feedback.message)
	}
}

func TestPlainCommandFeedbackIsPrintedWithoutEnteringConversationHistory(t *testing.T) {
	var screenOutput bytes.Buffer
	self := slashCommandFixture(t, caps.Read)
	self.screen = output.New(&screenOutput)
	self.isPlain = true
	self.commands = fixtureCommandRegistry(t, slash.Command{
		Name: "help",
		Run: func(context slash.Context, _ slash.Arguments) error {
			context.Notice("Commands:\n  /help")
			return nil
		},
	})

	if got := self.handleCommand("/help"); got != dispatch.Handled {
		t.Fatalf("got slash input result %d", got)
	}
	self.screen.End()

	if len(self.events) != 0 {
		t.Errorf("plain command feedback entered conversation events: %+v", self.events)
	}
	if !strings.Contains(style.Plain(screenOutput.String()), "Commands:\n  /help") {
		t.Errorf("plain command feedback was not printed: %q", style.Plain(screenOutput.String()))
	}
}

func TestUnknownSlashCommandShowsOneErrorWhileReturnRepeatsAndKeepsTheInput(t *testing.T) {
	self := slashCommandFixture(t, caps.Read)
	self.screen = output.New(&bytes.Buffer{})
	self.commands = fixtureCommandRegistry(t)
	editor := edit.NewInput(nil)
	for _, value := range "/unknown" {
		editor.Apply(key.Key{Code: key.Rune, Value: value}, false)
	}

	for range 100 {
		self.apply(editor, nil, key.Key{Code: key.Enter})
	}

	if got := editor.Text(); got != "/unknown" {
		t.Errorf("got input %q", got)
	}
	if len(self.events) != 0 {
		t.Fatalf("repeated command errors entered conversation events: %v", self.events)
	}
	want := "Command not found: /unknown (alt+enter sends as message)"
	if self.feedback.message.text != want || self.feedback.message.status != agent.ErrorStatus {
		t.Errorf("got feedback %+v, want failed %q", self.feedback.message, want)
	}
}

func TestUnknownSlashCommandDoesNotInterruptARunningTurn(t *testing.T) {
	var screenOutput bytes.Buffer
	self := slashCommandFixture(t, caps.Read)
	self.screen = output.New(&screenOutput)
	self.commands = fixtureCommandRegistry(t)
	self.currentTurn = Turn{Stream: testRunningTurnStream(), painter: self.newPainter(true)}
	self.currentTurn.painter.DrawDelta(agent.Delta{Kind: agent.ModelReasoningEvent, Text: "still working"})
	editor := edit.NewInput(nil)
	for _, value := range "/unknown" {
		editor.Apply(key.Key{Code: key.Rune, Value: value}, true)
	}

	self.acceptInput(editor, nil)

	if self.currentTurn.Cancelled() {
		t.Error("expected the running turn not to be interrupted")
	}
	self.currentTurn.painter.DrawDelta(agent.Delta{Kind: agent.ModelReasoningEvent, Text: " on it"})
	self.currentTurn.painter.DrawEvent(agent.Event{Kind: agent.ModelReasoningEvent, Text: "still working on it"})
	self.screen.End()
	plainOutput := style.Plain(screenOutput.String())
	if self.feedback.message.text != "Command not found: /unknown (alt+enter sends as message)" {
		t.Errorf("command error was not retained as interface feedback: %+v", self.feedback.message)
	}
	if strings.Contains(plainOutput, "Command not found") {
		t.Errorf("command error entered conversation scrollback: %q", plainOutput)
	}
	if strings.Count(plainOutput, "still working") != 1 {
		t.Errorf("expected the completed reasoning once, got %q", plainOutput)
	}
}

func TestSnippetKeepsItsInvocationInHistoryAndQueuesItsRenderedPrompt(t *testing.T) {
	self := slashCommandFixture(t, caps.Read)
	self.commands = fixtureSnippetRegistry(t, map[string]snippets.Definition{
		"add": {Prompt: "Add the following:\n\n{{ .Arg }}", Arguments: snippets.ArgumentsRequired},
	})
	self.currentTurn = Turn{Stream: testTurnStream(nil, func(error) {}, turn.State{Running: true})}

	historyPath := filepath.Join(t.TempDir(), "history")
	history := edit.NewHistory(historyPath, historyLimit)
	editor := edit.NewInput(history)
	for _, value := range "//add review this" {
		editor.Apply(key.Key{Code: key.Rune, Value: value}, true)
	}
	self.acceptInput(editor, history)

	pending := self.queuedTurn.Peek()
	if !pending.Replacement || pending.Message != "Add the following:\n\nreview this" {
		t.Errorf("got queued turn %+v", pending)
	}
	body, err := os.ReadFile(historyPath) //nolint:gosec // the path is the test's own history file
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "//add review this\n" {
		t.Errorf("got history %q", body)
	}
	if editor.Text() != "" {
		t.Errorf("got editor text %q", editor.Text())
	}
}

func TestSnippetKeepsTheLayoutOfAPastedArgument(t *testing.T) {
	self := slashCommandFixture(t, caps.Read)
	self.commands = fixtureSnippetRegistry(t, map[string]snippets.Definition{
		"add": {Prompt: "Add the following:\n\n{{ .Arg }}", Arguments: snippets.ArgumentsRequired},
	})
	self.currentTurn = Turn{Stream: testTurnStream(nil, func(error) {}, turn.State{Running: true})}

	historyPath := filepath.Join(t.TempDir(), "history")
	history := edit.NewHistory(historyPath, historyLimit)
	editor := edit.NewInput(history)
	for _, value := range "//add " {
		editor.Apply(key.Key{Code: key.Rune, Value: value}, true)
	}

	editor.Apply(key.Key{Code: key.PasteStart}, true)
	for _, value := range "review this\n\n- one\n- two" {
		if value == '\n' {
			editor.Apply(key.Key{Code: key.Enter}, true)
			continue
		}
		editor.Apply(key.Key{Code: key.Rune, Value: value}, true)
	}
	editor.Apply(key.Key{Code: key.PasteEnd}, true)
	self.acceptInput(editor, history)

	pending := self.queuedTurn.Peek()
	want := "Add the following:\n\nreview this\n\n- one\n- two"
	if !pending.Replacement || pending.Message != want {
		t.Errorf("got queued turn %+v, want message %q", pending, want)
	}
	body, err := os.ReadFile(historyPath) //nolint:gosec // the path is the test's own history file
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `//add review this\n\n- one\n- two`+"\n" {
		t.Errorf("got history %q", body)
	}
}

func TestSnippetWithoutArgumentsShowsUsageAndKeepsTheInput(t *testing.T) {
	self := slashCommandFixture(t, caps.Read)
	self.screen = output.New(&bytes.Buffer{})
	self.commands = fixtureSnippetRegistry(t, map[string]snippets.Definition{
		"add": {Prompt: "Add the following:\n\n{{ .Arg }}", Arguments: snippets.ArgumentsRequired},
	})
	history := edit.NewHistory("", historyLimit)
	editor := edit.NewInput(history)
	for _, value := range "//add" {
		editor.Apply(key.Key{Code: key.Rune, Value: value}, false)
	}

	self.acceptInput(editor, history)

	if editor.Text() != "//add" {
		t.Errorf("got editor text %q", editor.Text())
	}
	if len(self.events) != 0 {
		t.Errorf("snippet usage entered conversation events: %+v", self.events)
	}
	if self.feedback.message.text != "Usage: //add <args> (alt+enter sends as message)" {
		t.Errorf("got feedback %+v", self.feedback.message)
	}
}

func TestPlainSnippetInputWaitsForTheRenderedPrompt(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	self.commands = fixtureSnippetRegistry(t, map[string]snippets.Definition{
		"ask": {Prompt: "Question: {{index .Args 0}} / {{.Arg}}", Arguments: snippets.ArgumentsRequired},
	})

	historyPath := filepath.Join(t.TempDir(), "history")
	history := edit.NewHistory(historyPath, historyLimit)
	self.acceptPlainInput(history, "//ask why now")

	var userMessages []string
	for _, event := range self.events {
		if event.Kind == agent.UserMessageEvent {
			userMessages = append(userMessages, event.Text)
		}
	}
	if !slices.Equal(userMessages, []string{"Question: why / why now"}) {
		t.Errorf("got user messages %q", userMessages)
	}
	body, err := os.ReadFile(historyPath) //nolint:gosec // the path is the test's own history file
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "//ask why now\n" {
		t.Errorf("got history %q", body)
	}
}

func TestSnippetTemplateErrorsAreReportedAndKeepTheInput(t *testing.T) {
	self := slashCommandFixture(t, caps.Read)
	self.screen = output.New(&bytes.Buffer{})
	self.commands = fixtureSnippetRegistry(t, map[string]snippets.Definition{
		"review": {Prompt: "{{index .Args 2}}", Arguments: snippets.ArgumentsRequired},
	})

	if got := self.handleCommand("//review only-one"); got != dispatch.Rejected {
		t.Fatalf("got slash input result %d", got)
	}
	if len(self.events) != 0 {
		t.Errorf("snippet error entered conversation events: %+v", self.events)
	}
	if !strings.HasPrefix(self.feedback.message.text, "//review: Could not render template:") {
		t.Errorf("got feedback %+v", self.feedback.message)
	}
	if !strings.HasSuffix(self.feedback.message.text, " (alt+enter sends as message)") {
		t.Errorf("expected the way out to be offered, got %q", self.feedback.message.text)
	}
}

func TestUnknownSnippetShowsAnErrorAndKeepsTheInput(t *testing.T) {
	self := slashCommandFixture(t, caps.Read)
	self.screen = output.New(&bytes.Buffer{})
	self.commands = fixtureSnippetRegistry(t, nil)
	editor := edit.NewInput(nil)
	for _, value := range "//unknown" {
		editor.Apply(key.Key{Code: key.Rune, Value: value}, false)
	}

	self.acceptInput(editor, nil)

	if got := editor.Text(); got != "//unknown" {
		t.Errorf("got input %q", got)
	}
	want := "Snippet not found: //unknown (alt+enter sends as message)"
	if len(self.events) != 0 {
		t.Errorf("snippet error entered conversation events: %+v", self.events)
	}
	if self.feedback.message.text != want || self.feedback.message.status != agent.ErrorStatus {
		t.Errorf("got feedback %+v, want failed %q", self.feedback.message, want)
	}
}

func TestTabCompletionKeepsCommandNamespacesSeparate(t *testing.T) {
	systemSet, err := slash.NewCommandSet(
		"/",
		slash.Command{Name: "conf", Run: slashTestHandler},
		slash.Command{Name: "copy", Run: slashTestHandler},
	)
	if err != nil {
		t.Fatal(err)
	}
	snippetSet, err := snippets.New(map[string]snippets.Definition{
		"test":   {Prompt: "Run tests.", Arguments: snippets.ArgumentsNone},
		"review": {Prompt: "Review changes.", Arguments: snippets.ArgumentsNone},
	})
	if err != nil {
		t.Fatal(err)
	}
	self := slashCommandFixture(t, caps.Read)
	self.commands = fixtureRegistry(t, systemSet, snippetSet)

	for input, want := range map[string]string{
		"/":  "/conf",
		"//": "//help",
	} {
		self.completion.Reset()
		editor := edit.NewInput(nil)
		for _, value := range input {
			editor.Apply(key.Key{Code: key.Rune, Value: value}, false)
		}
		self.apply(editor, nil, key.Key{Code: key.Rune, Value: '\t'})
		if got := editor.Text(); got != want {
			t.Errorf("completion for %q got %q, want %q", input, got, want)
		}
	}
}

func TestTabCompletesAUniqueSlashCommand(t *testing.T) {
	self := slashCommandFixture(t, caps.Read)
	self.commands = fixtureCommandRegistry(
		t,
		slash.Command{Name: "conf", Run: slashTestHandler},
		slash.Command{Name: "copy", Run: slashTestHandler},
		slash.Command{Name: "open", Run: slashTestHandler},
	)
	editor := edit.NewInput(nil)
	for _, value := range "/op" {
		editor.Apply(key.Key{Code: key.Rune, Value: value}, false)
	}

	self.apply(editor, nil, key.Key{Code: key.Rune, Value: '\t'})

	if got := editor.Text(); got != "/open" {
		t.Errorf("got completion %q", got)
	}
}

func slashTestHandler(slash.Context, slash.Arguments) error {
	return nil
}

func fixtureCommandRegistry(t *testing.T, commands ...slash.Command) slash.Registry {
	t.Helper()

	set, err := slash.NewCommandSet("/", commands...)
	if err != nil {
		t.Fatal(err)
	}
	return fixtureRegistry(t, set)
}

func fixtureSnippetRegistry(t *testing.T, configured map[string]snippets.Definition) slash.Registry {
	t.Helper()

	systemSet, err := slash.NewCommandSet("/")
	if err != nil {
		t.Fatal(err)
	}
	snippetSet, err := snippets.New(configured)
	if err != nil {
		t.Fatal(err)
	}
	return fixtureRegistry(t, systemSet, snippetSet)
}

func fixtureRegistry(t *testing.T, sets ...slash.CommandSet) slash.Registry {
	t.Helper()

	registry, err := slash.NewRegistry(sets...)
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func slashCommandFixture(t *testing.T, currentCaps caps.Set) *App {
	t.Helper()

	log, err := store.Create(t.TempDir(), store.Meta{Model: "gpt"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })

	return &App{
		recorder: record.New(log),
		mode:     caps.NewMode(currentCaps),
	}
}

func TestConsecutiveTabsCycleCommandArguments(t *testing.T) {
	self := slashCommandFixture(t, caps.Read)
	self.commands = fixtureCommandRegistry(
		t,
		slash.Command{Name: "copy", Run: slashTestHandler}.WithArguments("session-name", "session-id", "session-dir"),
	)
	editor := edit.NewInput(nil)
	for _, value := range "/copy " {
		editor.Apply(key.Key{Code: key.Rune, Value: value}, false)
	}

	self.apply(editor, nil, key.Key{Code: key.Rune, Value: '\t'})
	if got := editor.Text(); got != "/copy session-dir" {
		t.Errorf("got first completion %q", got)
	}

	self.apply(editor, nil, key.Key{Code: key.Rune, Value: '\t'})
	if got := editor.Text(); got != "/copy session-id" {
		t.Errorf("got second completion %q", got)
	}
}

func TestARequestedTransitionStopsTheApp(t *testing.T) {
	self := slashCommandFixture(t, caps.Read)
	transition := cycle.Transition{Kind: cycle.NewSession, Arguments: []string{"-m", "codex/gpt@high"}}
	if err := self.requestTransition(transition); err != nil {
		t.Fatal(err)
	}
	if self.apply(edit.NewInput(nil), nil, key.Key{}) {
		t.Error("expected the app to stop")
	}
	if !slices.Equal(self.transition.Arguments, transition.Arguments) {
		t.Errorf("got transition %+v", self.transition)
	}
}

func TestSlashCommandCanAddSuccessFeedback(t *testing.T) {
	fixtureCommands := fixtureCommandRegistry(t, slash.Command{
		Name: "fixture",
		Run: func(context slash.Context, _ slash.Arguments) error {
			context.Success("Copied to clipboard.")
			return nil
		},
	})

	self := slashCommandFixture(t, caps.Read)
	self.screen = output.New(&bytes.Buffer{})
	self.commands = fixtureCommands
	if got := self.handleCommand("/fixture"); got != dispatch.Handled {
		t.Fatalf("got slash input result %d", got)
	}
	if len(self.events) != 0 {
		t.Errorf("success feedback entered conversation events: %+v", self.events)
	}
	if self.feedback.message.text != "Copied to clipboard." || self.feedback.message.status != agent.SuccessStatus {
		t.Errorf("got feedback %+v", self.feedback.message)
	}
}

func TestUsageErrorIsNotPrefixedWithTheCommandName(t *testing.T) {
	self := slashCommandFixture(t, caps.Read)
	self.screen = output.New(&bytes.Buffer{})
	self.commands = fixtureCommandRegistry(t, slash.Command{
		Name: "copy",
		Run: func(slash.Context, slash.Arguments) error {
			return slash.Usage()
		},
	}.WithArguments("session-name", "session-id", "session-dir"))

	if got := self.handleCommand("/copy"); got != dispatch.Rejected {
		t.Fatalf("got slash input result %d", got)
	}
	want := "Usage: /copy {session-dir|session-id|session-name} (alt+enter sends as message)"
	if len(self.events) != 0 {
		t.Errorf("usage feedback entered conversation events: %+v", self.events)
	}
	if self.feedback.message.text != want {
		t.Errorf("got feedback %+v, want %q", self.feedback.message, want)
	}
}

func TestARefusedCommandKeepsWhatWasTypedAndSaysWhy(t *testing.T) {
	self := slashCommandFixture(t, caps.Read)
	self.screen = output.New(&bytes.Buffer{})
	self.commands = fixtureCommandRegistry(t, slash.Command{
		Name: "new",
		Run: func(slash.Context, slash.Arguments) error {
			return errors.New(`model "opus" is ambiguous`)
		},
	})

	editor := edit.NewInput(nil)
	for _, value := range "/new opus" {
		editor.Apply(key.Key{Code: key.Rune, Value: value}, false)
	}

	self.acceptInput(editor, edit.NewHistory("", historyLimit))

	if got := editor.Text(); got != "/new opus" {
		t.Errorf("expected the refused command to survive, got %q", got)
	}

	want := `/new: Model "opus" is ambiguous (alt+enter sends as message)`
	if len(self.events) != 0 {
		t.Errorf("command feedback entered conversation events: %+v", self.events)
	}
	if self.feedback.message.text != want {
		t.Errorf("got feedback %+v, want %q", self.feedback.message, want)
	}
}

const sessionGoldenSystemPrompt = "You are a test assistant."

type sessionGoldenResponse struct {
	Events               []string          `toml:"event"`
	Lines                []string          `toml:"line"`
	Body                 string            `toml:"body"`
	Headers              map[string]string `toml:"headers"`
	Status               int               `toml:"status"`
	Repeat               int               `toml:"repeat"`
	RefuseConnection     bool              `toml:"refuse-connection"`
	CancelAfterWireEvent int               `toml:"cancel-after-wire-event"`
	ResetAfterWireEvent  int               `toml:"reset-after-wire-event"`
	WaitForCancellation  bool              `toml:"wait-for-cancellation"`
}

type sessionGoldenTurn struct {
	Prompt                    string                  `toml:"prompt"`
	Responses                 []sessionGoldenResponse `toml:"response"`
	Timeout                   string                  `toml:"timeout"`
	IsCancelled               bool                    `toml:"is-cancelled"`
	CancelWithCtrlD           bool                    `toml:"cancel-with-ctrl-d"`
	CancelAfterReasoningDelta int                     `toml:"cancel-after-reasoning-delta"`
	CancelAfterReasoningEvent int                     `toml:"cancel-after-reasoning-event"`
	CancelAfterMessageDelta   int                     `toml:"cancel-after-message-delta"`
	CancelAfterToolRequest    int                     `toml:"cancel-after-tool-request"`
	CancelAfterRetryNotice    int                     `toml:"cancel-after-retry-notice"`
	ReplaceAfterToolRequest   string                  `toml:"replace-after-tool-request"`
	ToggleAfterMessageDelta   string                  `toml:"toggle-after-message-delta"`
	ToggleAfterToolRequest    string                  `toml:"toggle-after-tool-request"`
	ToggleDuringModeTurn      string                  `toml:"toggle-during-mode-turn"`
	CancelAfterToolToggle     bool                    `toml:"cancel-after-tool-toggle"`
}

type sessionGoldenTool struct {
	Name          string   `toml:"name"`
	Outputs       []string `toml:"outputs"`
	StateKey      string   `toml:"state-key"`
	ShellWithheld bool     `toml:"shell-withheld"`
	WebWithheld   bool     `toml:"web-withheld"`
	WebAnswer     string   `toml:"web-answer"`
	Blocks        bool     `toml:"blocks"`
	StoppedOutput string   `toml:"stopped-output"`
}

type sessionGoldenScenario struct {
	Name              string              `toml:"-"`
	Provider          string              `toml:"provider"`
	Model             string              `toml:"model"`
	Effort            string              `toml:"effort"`
	FirstTokenError   string              `toml:"first-token-error"`
	ToggleBeforeFirst string              `toml:"toggle-before-first"`
	Tools             []sessionGoldenTool `toml:"tool"`
	FirstTurn         sessionGoldenTurn   `toml:"first"`
	ResumeTurn        sessionGoldenTurn   `toml:"resume"`
}

func TestScenariosProduceCanonicalOutputs(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("testdata", "scenarios", "*.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no session golden scenarios")
	}

	for _, path := range paths {
		scenario := readSessionGoldenScenario(t, path)
		t.Run(scenario.Name, func(t *testing.T) {
			for extension, got := range runSessionGoldenScenario(t, scenario) {
				compareScenarioGolden(t, scenario.Name+extension, got)
			}
		})
	}
}

func readSessionGoldenScenario(t *testing.T, path string) sessionGoldenScenario {
	t.Helper()

	var scenario sessionGoldenScenario
	metadata, err := toml.DecodeFile(path, &scenario)
	if err != nil {
		t.Fatal(err)
	}
	if undecodedKeys := metadata.Undecoded(); len(undecodedKeys) > 0 {
		t.Fatalf("%s: no such setting: %s", path, undecodedKeys[0])
	}

	scenario.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	return scenario
}

func compareScenarioGolden(t *testing.T, name string, got string) {
	t.Helper()

	goldenPath := filepath.Join("testdata", "output", name)
	if *updateGoldens {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o700); err != nil {
			t.Fatal(err)
		}
		//nolint:gosec // the path is built from the fixed testdata directory
		if err := os.WriteFile(goldenPath, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath) //nolint:gosec // fixed testdata path
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("output differs from %s\n--- got ---\n%s--- want ---\n%s", goldenPath, got, want)
	}
}

type failingAnthropicTokenSource struct {
	failure error
}

func (self failingAnthropicTokenSource) Token() (string, error) {
	return "", self.failure
}

func newSessionGoldenAnthropicTokenSource(tokenError string) anthropic.TokenSource {
	if tokenError != "" {
		return failingAnthropicTokenSource{failure: errors.New(tokenError)}
	}
	return anthropic.Static("test-token")
}

func sessionGoldenProviderFor(
	t *testing.T,
	scenario sessionGoldenScenario,
	endpoint string,
	tokenError string,
	sessionName string,
) agent.Provider {
	t.Helper()

	provider := newSessionGoldenProvider(t, scenario, endpoint, tokenError)
	if scoped, isScoped := provider.(interface{ UseSession(name string) }); isScoped {
		scoped.UseSession(sessionName)
	}

	return provider
}

func newSessionGoldenProvider(
	t *testing.T,
	scenario sessionGoldenScenario,
	endpoint string,
	tokenError string,
) agent.Provider {
	t.Helper()

	switch scenario.Provider {
	case "anthropic":
		tokens := newSessionGoldenAnthropicTokenSource(tokenError)
		client, err := anthropic.New(tokens, scenario.Model, scenario.Effort, 128_000)
		if err != nil {
			t.Fatal(err)
		}
		client.URL = endpoint
		return client
	case "codex":
		client, err := codex.New(codex.Static("test-token", "test-account"), scenario.Model, scenario.Effort)
		if err != nil {
			t.Fatal(err)
		}
		client.URL = endpoint
		return client
	case "chat":
		client, err := chatcompletions.New(
			endpoint,
			http.Header{"Authorization": {"Bearer test-token"}},
			scenario.Model,
			scenario.Effort,
			128_000,
		)
		if err != nil {
			t.Fatal(err)
		}
		return client
	case "ollama":
		client, err := ollama.New(endpoint, scenario.Model, scenario.Effort, 32_768)
		if err != nil {
			t.Fatal(err)
		}
		return client
	default:
		t.Fatalf("unknown provider %q", scenario.Provider)
		return nil
	}
}

func newSessionGoldenTools(t *testing.T, specifications []sessionGoldenTool) []tool.Tool {
	t.Helper()

	tools := make([]tool.Tool, 0, len(specifications))
	for _, specification := range specifications {
		if specification.ShellWithheld {
			tools = append(tools, newSessionGoldenWithheldShell(t))
			continue
		}

		if specification.Name == title.Name {
			tools = append(tools, title.New())
			continue
		}

		if specification.WebWithheld || specification.WebAnswer != "" {
			isGranted := specification.WebAnswer != ""
			searcher := sessionGoldenSearcher{answer: specification.WebAnswer}
			tools = append(tools, web.New(func() bool { return isGranted }, searcher)...)
			continue
		}

		if specification.Blocks {
			tools = append(tools, newSessionGoldenBlockingTool(specification))
			continue
		}

		callCount := 0
		builder := tool.Implement(
			tool.Definition{Name: specification.Name, Description: "A deterministic scenario tool."},
			func(struct{}) (string, string) { return specification.Name, "" },
		)
		if specification.StateKey != "" {
			builder = builder.State(specification.StateKey, func(state json.RawMessage) error {
				return json.Unmarshal(state, &callCount)
			})
		}
		tools = append(tools, builder.Run(func(context.Context, struct{}) (tool.ToolCallResult, error) {
			if callCount >= len(specification.Outputs) {
				return tool.ToolCallResult{}, fmt.Errorf("tool %s has no output for call %d", specification.Name, callCount+1)
			}

			output := specification.Outputs[callCount]
			callCount++
			state, err := json.Marshal(callCount)
			if err != nil {
				return tool.ToolCallResult{}, err
			}
			return tool.ToolCallResult{Output: output, State: state}, nil
		}))
	}
	return tools
}

var errSessionGoldenToolStopped = errors.New("the tool was stopped")

func newSessionGoldenBlockingTool(specification sessionGoldenTool) tool.Tool {
	return tool.Implement(
		tool.Definition{Name: specification.Name, Description: "A scenario tool that blocks."},
		func(struct{}) (string, string) { return specification.Name, "" },
	).Run(func(ctx context.Context, _ struct{}) (tool.ToolCallResult, error) {
		<-ctx.Done()
		return tool.ToolCallResult{Output: specification.StoppedOutput}, errSessionGoldenToolStopped
	})
}

type sessionGoldenSearcher struct {
	answer string
}

func (self sessionGoldenSearcher) Search(context.Context, string) (string, error) {
	return self.answer, nil
}

func newSessionGoldenWithheldShell(t *testing.T) tool.Tool {
	t.Helper()

	workspace := t.TempDir()
	workspaceRoot, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workspaceRoot.Close() })

	files := file.New(workspaceRoot, func(string) error { return file.ErrReadOnly })
	mode := caps.NewMode(caps.Read)

	return shell.New(workspace, t.TempDir(), t.TempDir(), shell.Paths{}, mode, files)
}

func serveSessionGoldenResponse(
	writer http.ResponseWriter,
	request *http.Request,
	response sessionGoldenResponse,
	cancelSignals chan<- struct{},
) {
	if response.RefuseConnection {
		resetSessionGoldenConnection(writer)
		return
	}

	writer.Header().Set("Content-Type", "text/event-stream")
	for name, value := range response.Headers {
		writer.Header().Set(name, value)
	}
	if response.Status != 0 && response.Status != http.StatusOK {
		writer.WriteHeader(response.Status)
		_, _ = fmt.Fprint(writer, response.Body)
		return
	}

	flusher, canFlush := writer.(http.Flusher)
	if !canFlush {
		panic("httptest response cannot flush")
	}

	for _, line := range response.Lines {
		_, _ = fmt.Fprintln(writer, line)
		flusher.Flush()
	}
	for i, payload := range response.Events {
		_, _ = fmt.Fprintf(writer, "data: %s\n\n", payload)
		flusher.Flush()
		switch i + 1 {
		case response.CancelAfterWireEvent:
			cancelSignals <- struct{}{}
			<-request.Context().Done()
			return
		case response.ResetAfterWireEvent:
			resetSessionGoldenConnection(writer)
			return
		}
	}
	if response.WaitForCancellation {
		<-request.Context().Done()
	}
}

func resetSessionGoldenConnection(writer http.ResponseWriter) {
	hijacker, canHijack := writer.(http.Hijacker)
	if !canHijack {
		panic("httptest response cannot be hijacked")
	}
	connection, _, err := hijacker.Hijack()
	if err != nil {
		panic(err)
	}
	_ = connection.Close()
}

func expandSessionGoldenResponses(responses []sessionGoldenResponse) []sessionGoldenResponse {
	expanded := make([]sessionGoldenResponse, 0, len(responses))

	for _, response := range responses {
		expanded = append(expanded, slices.Repeat([]sessionGoldenResponse{response}, max(response.Repeat, 1))...)
	}

	return expanded
}

func runSessionGoldenScenario(t *testing.T, scenario sessionGoldenScenario) map[string]string {
	t.Helper()

	responses := expandSessionGoldenResponses(
		append(slices.Clone(scenario.FirstTurn.Responses), scenario.ResumeTurn.Responses...),
	)
	cancelSignals := make(chan struct{}, len(responses))
	var requestCount atomic.Int32
	var requestMutex sync.Mutex
	var requestBodies [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestBody, err := io.ReadAll(request.Body)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		requestMutex.Lock()
		requestBodies = append(requestBodies, requestBody)
		requestMutex.Unlock()

		requestIndex := int(requestCount.Add(1) - 1)
		if requestIndex >= len(responses) {
			http.Error(writer, "scenario has no response for this request", http.StatusConflict)
			return
		}
		serveSessionGoldenResponse(writer, request, responses[requestIndex], cancelSignals)
	}))
	defer server.Close()

	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{
		Model:        scenario.Model,
		Provider:     scenario.Provider,
		Effort:       scenario.Effort,
		SystemPrompt: sessionGoldenSystemPrompt,
	})
	if err != nil {
		t.Fatal(err)
	}

	firstAssistant := agent.New(
		sessionGoldenSystemPrompt,
		sessionGoldenProviderFor(
			t, scenario, server.URL, scenario.FirstTokenError, log.Name(),
		),
		newSessionGoldenTools(t, scenario.Tools),
	)
	firstAssistant.TakeRetryWaitsAtOnce()
	var firstScreenOutput bytes.Buffer
	firstHarness := &App{
		agent:    firstAssistant,
		screen:   output.NewTerminalOfSize(&firstScreenOutput, replayColumns, replayLines),
		recorder: record.New(log),
	}
	settleSessionGoldenMode(firstHarness)
	if scenario.ToggleBeforeFirst != "" {
		toggleSessionGoldenCaps(t, firstHarness, scenario.ToggleBeforeFirst)
		firstHarness.settleMode()
		firstAssistant.AddUserMessage(firstHarness.mode.Inject())
	}
	firstHarness.currentTurn = Turn{Stream: testRunningTurnStream(), painter: firstHarness.newPainter(true)}
	runSessionGoldenTurn(t, firstHarness, scenario.FirstTurn, cancelSignals)
	firstHarness.dropPendingInput()

	sessionName := log.Name()
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	storedSession, err := store.Read(directory, sessionName)
	if err != nil {
		t.Fatal(err)
	}
	resumedAssistant := agent.New(
		storedSession.Meta.SystemPrompt,
		sessionGoldenProviderFor(t, scenario, server.URL, "", sessionName),
		newSessionGoldenTools(t, scenario.Tools),
	)
	resumedAssistant.TakeRetryWaitsAtOnce()
	if err := resumedAssistant.RestoreState(storedSession.Events); err != nil {
		t.Fatal(err)
	}
	if err := resumedAssistant.Load(storedSession.Items); err != nil {
		t.Fatal(err)
	}

	log, err = store.Open(directory, sessionName)
	if err != nil {
		t.Fatal(err)
	}
	var screenOutput bytes.Buffer
	resumedRecorder := record.New(log)
	resumedRecorder.Resume(len(storedSession.Items))
	resumedHarness := &App{
		agent:    resumedAssistant,
		screen:   output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines),
		recorder: resumedRecorder,
		events:   slices.Clone(storedSession.Events),
	}
	settleResumedSessionGoldenMode(resumedHarness, storedSession.Events)
	resumedHarness.currentTurn = Turn{Stream: testRunningTurnStream()}
	resumedHarness.replay()
	requireSameVisibleScreen(
		t,
		"first interactive turn differs after resume",
		firstScreenOutput.String(),
		screenOutput.String(),
	)
	if note := resumedHarness.prelude(); note != "" {
		resumedAssistant.AddUserMessage(note)
	}
	runSessionGoldenTurn(t, resumedHarness, scenario.ResumeTurn, cancelSignals)
	resumedHarness.dropPendingInput()

	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	if got := int(requestCount.Load()); got != len(responses) {
		t.Errorf("provider received %d requests, want %d", got, len(responses))
	}

	transcriptPath := filepath.Join(directory, sessionName, "chat.md")
	transcript, err := os.ReadFile(transcriptPath) //nolint:gosec // the path is the test's own session directory
	if err != nil {
		t.Fatal(err)
	}

	storedSession, err = store.Read(directory, sessionName)
	if err != nil {
		t.Fatal(err)
	}
	var replayOutput bytes.Buffer
	replayHarness := &App{
		agent:  resumedAssistant,
		screen: output.NewTerminalOfSize(&replayOutput, replayColumns, replayLines),
		events: storedSession.Events,
	}
	replayHarness.replay()

	requireSameVisibleScreen(
		t,
		"resumed live screen differs from its stored replay",
		screenOutput.String(),
		replayOutput.String(),
	)
	liveScreen := visibleScreen(t, screenOutput.String(), replayColumns)

	ansi := strings.TrimRight(strutil.VisibleEscapes(screenOutput.String()), "\n") + "\n"
	settledScreen := strings.Join(liveScreen, "\n") + "\n"

	requestMutex.Lock()
	capturedRequestBodies := slices.Clone(requestBodies)
	requestMutex.Unlock()
	requests := canonicalProviderRequests(t, capturedRequestBodies)

	outputs := map[string]string{
		".jsonl":          canonicalSessionJournal(t, directory, sessionName),
		".meta.json":      canonicalSessionMeta(t, directory, sessionName),
		".ansi":           ansi,
		".screen":         settledScreen,
		".transcript":     canonicalSessionTranscript(string(transcript), sessionName),
		".requests.jsonl": requests,
	}

	for extension, drawn := range outputs {
		outputs[extension] = endpointAddressPattern.ReplaceAllString(drawn, "http://endpoint")
	}

	return outputs
}

func canonicalProviderRequests(t *testing.T, requestBodies [][]byte) string {
	t.Helper()

	cacheKeys := map[string]string{}

	var canonical bytes.Buffer
	for _, requestBody := range requestBodies {
		var request map[string]any
		if err := json.Unmarshal(requestBody, &request); err != nil {
			t.Fatal(err)
		}

		if key, isKeyed := request["prompt_cache_key"].(string); isKeyed {
			if _, isSeen := cacheKeys[key]; !isSeen {
				cacheKeys[key] = fmt.Sprintf("session-%d", len(cacheKeys)+1)
			}
			request["prompt_cache_key"] = cacheKeys[key]
		}
		encoded, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		canonical.Write(encoded)
		canonical.WriteByte('\n')
	}
	return canonical.String()
}

func requireSameVisibleScreen(t *testing.T, description string, firstOutput string, secondOutput string) {
	t.Helper()

	firstScreen := visibleScreen(t, firstOutput, replayColumns)
	secondScreen := visibleScreen(t, secondOutput, replayColumns)
	if !slices.Equal(firstScreen, secondScreen) {
		t.Errorf(
			"%s\nfirst:\n%s\nsecond:\n%s",
			description,
			strings.Join(firstScreen, "\n"),
			strings.Join(secondScreen, "\n"),
		)
	}
}

func settleSessionGoldenMode(testHarness *App) {
	testHarness.mode = caps.NewMode(caps.All())
	testHarness.settleMode()
}

func settleResumedSessionGoldenMode(testHarness *App, events []agent.Event) {
	resumedCaps, found := caps.LastRecordedMode(events)
	if !found {
		resumedCaps = caps.All()
	}

	testHarness.mode = caps.NewMode(resumedCaps)
	testHarness.settledCaps = resumedCaps
}

func toggleSessionGoldenCaps(t *testing.T, testHarness *App, flags string) {
	t.Helper()

	for _, flag := range flags {
		toggledCaps, known := caps.Named(string(flag))
		if !known {
			t.Fatalf("unknown capability flag %q", string(flag))
		}
		testHarness.toggleCap(toggledCaps)
	}
}

func runSessionGoldenTurn(
	t *testing.T,
	testHarness *App,
	turn sessionGoldenTurn,
	cancelSignals <-chan struct{},
) {
	t.Helper()

	var streamContext context.Context
	var cancel context.CancelCauseFunc
	if turn.Timeout == "" {
		streamContext, cancel = context.WithCancelCause(t.Context())
	} else {
		timeout, err := time.ParseDuration(turn.Timeout)
		if err != nil {
			t.Fatal(err)
		}
		timed, stopTimer := context.WithTimeout(t.Context(), timeout)
		defer stopTimer()
		streamContext, cancel = context.WithCancelCause(timed)
	}
	defer cancel(nil)
	testHarness.currentTurn.Stream = testRunningTurnStreamWithCancel(cancel)
	editor := edit.NewInput(nil)
	stopKey := key.Key{Code: key.Escape}
	if turn.CancelWithCtrlD {
		stopKey = key.Key{Code: key.Rune, Value: 'd', Mod: key.Ctrl}
	}
	interruptWithStopKey := func() {
		if !testHarness.apply(editor, nil, stopKey) {
			t.Fatal("the stop key closed the harness")
		}
	}

	go func() {
		select {
		case <-cancelSignals:
			cancel(nil)
		case <-streamContext.Done():
		}
	}()

	takeToolRequest := func(toolRequests int) {
		if toolRequests == turn.CancelAfterToolRequest {
			interruptWithStopKey()
		}
		if toolRequests != 1 {
			return
		}
		if turn.ReplaceAfterToolRequest != "" {
			testHarness.replaceTurn(turn.ReplaceAfterToolRequest)
		}
		if turn.ToggleAfterToolRequest != "" {
			toggleSessionGoldenCaps(t, testHarness, turn.ToggleAfterToolRequest)
			if turn.CancelAfterToolToggle {
				interruptWithStopKey()
			}
		}
	}

	reasoningDeltas := 0
	reasoningEvents := 0
	messageDeltas := 0
	toolRequests := 0
	retryNotices := 0
	for update, streamError := range testHarness.agent.Stream(streamContext, turn.Prompt) {
		testHarness.takeTurn(TurnEvent{Update: update, Err: streamError})
		if update.Delta != nil {
			switch update.Delta.Kind {
			case agent.ModelReasoningEvent:
				reasoningDeltas++
				if reasoningDeltas == turn.CancelAfterReasoningDelta {
					interruptWithStopKey()
				}
			case agent.ModelMessageEvent:
				messageDeltas++
				if messageDeltas == turn.CancelAfterMessageDelta {
					interruptWithStopKey()
				}
				if messageDeltas == 1 && turn.ToggleAfterMessageDelta != "" {
					toggleSessionGoldenCaps(t, testHarness, turn.ToggleAfterMessageDelta)
				}
			}
		}
		if update.Event != nil && update.Event.Kind == agent.ModelReasoningEvent {
			reasoningEvents++
			if reasoningEvents == turn.CancelAfterReasoningEvent {
				interruptWithStopKey()
			}
		}
		if update.Event != nil && update.Event.Kind == agent.RetryingEvent {
			retryNotices++
			if retryNotices == turn.CancelAfterRetryNotice {
				interruptWithStopKey()
			}
		}
		if update.Event != nil && update.Event.Kind == agent.ToolCallRequestEvent {
			toolRequests++
			takeToolRequest(toolRequests)
		}
	}
	if turn.IsCancelled && !testHarness.currentTurn.Cancelled() {
		testHarness.interruptTurn(stopKeyReason(stopKey))
	}
	testHarness.finish()

	runQueuedSessionGoldenTurns(t, testHarness, turn.ToggleDuringModeTurn)
}

func runQueuedSessionGoldenTurns(t *testing.T, testHarness *App, toggleDuringModeTurn string) {
	t.Helper()

	for testHarness.currentTurn.Running() {
		for event := range testHarness.currentTurn.Events() {
			testHarness.takeTurn(event)
			if toggleDuringModeTurn == "" || event.Update.Delta == nil {
				continue
			}
			if event.Update.Delta.Kind == agent.ModelMessageEvent {
				toggleSessionGoldenCaps(t, testHarness, toggleDuringModeTurn)
				toggleDuringModeTurn = ""
			}
		}
		testHarness.finish()
	}
}

func canonicalSessionMeta(t *testing.T, directory string, name string) string {
	t.Helper()

	encoded, err := os.ReadFile(filepath.Join(directory, name, "meta.json")) //nolint:gosec // the path is the test's own session directory
	if err != nil {
		t.Fatal(err)
	}

	var meta map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &meta); err != nil {
		t.Fatal(err)
	}
	canonicalName, err := json.Marshal("brave-otter")
	if err != nil {
		t.Fatal(err)
	}
	canonicalTime, err := json.Marshal(transcriptTime)
	if err != nil {
		t.Fatal(err)
	}
	meta["name"] = canonicalName
	meta["started"] = canonicalTime
	meta["touched"] = canonicalTime

	canonical, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	return string(append(canonical, '\n'))
}

func canonicalSessionJournal(t *testing.T, directory string, name string) string {
	t.Helper()

	var canonical bytes.Buffer
	err := session.Records(directory, name, func(line session.Line) error {
		var event *agent.Event
		if line.Event != nil {
			canonicalEvent := *line.Event
			canonicalEvent.Took = 0
			event = &canonicalEvent
		}

		record := struct {
			Kind    session.Kind    `json:"kind"`
			Version int             `json:"version,omitempty"`
			Meta    json.RawMessage `json:"meta,omitempty"`
			Event   *agent.Event    `json:"event,omitempty"`
			Payload json.RawMessage `json:"payload,omitempty"`
		}{
			Kind:    line.Kind,
			Version: line.Version,
			Meta:    line.Meta,
			Event:   event,
			Payload: line.Payload,
		}

		encoded, err := json.Marshal(record)
		if err != nil {
			return err
		}
		canonical.Write(encoded)
		canonical.WriteByte('\n')
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	return canonical.String()
}

var (
	endpointAddressPattern    = regexp.MustCompile(`http://127\.0\.0\.1:\d+`)
	transcriptStartedPattern  = regexp.MustCompile(`(?m)^- \*\*Started:\*\* ` + "`[^`]+`$")
	transcriptEventPattern    = regexp.MustCompile(`(?m)^> [^\n]+$`)
	transcriptDurationPattern = regexp.MustCompile(`(?m)^- \*\*Duration:\*\* ` + "`[^`]+`$")
	transcriptElapsedPattern  = regexp.MustCompile(`(?m)(^## .+ · )\+[^ \n]+$`)
)

func canonicalSessionTranscript(transcript string, sessionName string) string {
	canonical := strings.ReplaceAll(transcript, sessionName, "brave-otter")
	canonical = transcriptStartedPattern.ReplaceAllString(
		canonical,
		"- **Started:** `"+transcriptTime.Format(time.RFC3339Nano)+"`",
	)
	canonical = transcriptDurationPattern.ReplaceAllString(canonical, "- **Duration:** `0s`")
	canonical = transcriptElapsedPattern.ReplaceAllString(canonical, "${1}+0s")

	eventIndex := 0
	return transcriptEventPattern.ReplaceAllStringFunc(canonical, func(string) string {
		when := transcriptTime.Add(time.Duration(eventIndex) * time.Second)
		eventIndex++
		return "> " + when.Format(time.RFC3339Nano)
	})
}

func TestCanonicalTranscriptElapsedTimeDoesNotDependOnTestExecutionSpeed(t *testing.T) {
	transcript := "## Assistant · +0.1s\n"
	if got := canonicalSessionTranscript(transcript, "session"); got != "## Assistant · +0s\n" {
		t.Errorf("got %q", got)
	}
}

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

func newStorageFaultHarness(log SessionLogger, assistant *agent.Agent) *App {
	testHarness := &App{
		agent:    assistant,
		screen:   output.New(&bytes.Buffer{}),
		recorder: record.New(log),
		mode:     caps.NewMode(caps.All()),
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
	if len(storedSession.Events) != 1 || storedSession.Events[0].Kind != agent.UserMessageEvent {
		t.Errorf("unexpected canonical events: %+v", storedSession.Events)
	}
	if testHarness.feedback.message.status != agent.ErrorStatus || !strings.Contains(testHarness.feedback.message.text, "item write failed") {
		t.Errorf("provider item failure was not retained as interface feedback: %+v", testHarness.feedback.message)
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
	if len(storedSession.Events) != 1 || storedSession.Events[0].Kind != agent.UserMessageEvent {
		t.Errorf("unexpected canonical events: %+v", storedSession.Events)
	}
	for _, want := range []string{"transcript append failed", "wire append failed"} {
		if !strings.Contains(testHarness.feedback.message.text, want) {
			t.Errorf("interface feedback omitted %q: %+v", want, testHarness.feedback.message)
		}
	}
}

func testRunningTurnStream() *turn.Stream {
	return testTurnStream(nil, nil, turn.State{Running: true})
}

func testTurnStreamForRunning(isRunning bool) *turn.Stream {
	if !isRunning {
		return nil
	}
	return testRunningTurnStream()
}

func testTimedTurnStream(isRunning bool, startedAt time.Time, finishedAt time.Time) *turn.Stream {
	if isRunning {
		finishedAt = time.Time{}
	}
	return testTurnStream(nil, nil, turn.State{Running: isRunning, StartedAt: startedAt, FinishedAt: finishedAt})
}

func testRunningTurnStreamWithCancel(cancel context.CancelCauseFunc) *turn.Stream {
	return testTurnStream(nil, cancel, turn.State{Running: true})
}

func testTurnStream(events chan TurnEvent, cancel context.CancelCauseFunc, state turn.State) *turn.Stream {
	if events == nil {
		events = make(chan TurnEvent)
	}
	if cancel == nil {
		cancel = func(error) {}
	}
	if state.StartedAt.IsZero() {
		state.StartedAt = time.Now()
	}
	return turn.Adopt(events, cancel, state)
}

func TestTheTimerRetainsTheModelTurnWhileTheUserTurnCountsUp(t *testing.T) {
	finishedAt := time.Now().Add(-69 * time.Second)
	harness := App{currentTurn: Turn{
		Stream: testTimedTurnStream(false, finishedAt.Add(-time.Minute), finishedAt),
	}}

	got := harness.turnTiming()
	want := turn.Timing{UserTurn: 69 * time.Second, ModelTurn: time.Minute}
	if got.UserTurn.Round(time.Second) != want.UserTurn || got.ModelTurn != want.ModelTurn {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestTheTimerRetainsTheUserTurnWhileTheModelTurnCountsUp(t *testing.T) {
	startedAt := time.Now().Add(-30 * time.Second)
	harness := App{currentTurn: Turn{Stream: testTurnStream(nil, nil, turn.State{
		Running:   true,
		StartedAt: startedAt,
		Timing:    turn.Timing{UserTurn: 3 * time.Minute},
	})}}

	got := harness.turnTiming()
	want := turn.Timing{UserTurn: 3 * time.Minute, ModelTurn: 30 * time.Second}
	if got.UserTurn != want.UserTurn || got.ModelTurn.Round(time.Second) != want.ModelTurn {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestTheTimerCountsUpFromTheSessionBeforeTheFirstTurn(t *testing.T) {
	harness := App{startedAt: time.Now().Add(-30 * time.Second)}

	got := harness.turnTiming()
	if got.UserTurn.Round(time.Second) != 30*time.Second || got.ModelTurn != 0 {
		t.Errorf("got %+v, want a 30 second user turn", got)
	}
}

const (
	thinkingFor = 2 * time.Second
	wordEvery   = 120 * time.Millisecond
	toolTakes   = 3 * time.Second
)

type fakeProvider struct {
	turn  int
	items []json.RawMessage
}

func (self *fakeProvider) Configure(string, []tool.Definition)   {}
func (self *fakeProvider) AddUserMessage(string)                 {}
func (self *fakeProvider) AddToolResults([]agent.ToolCallResult) {}
func (self *fakeProvider) Dump() []json.RawMessage               { return self.items }
func (self *fakeProvider) Load(items []json.RawMessage)          { self.items = items }

func (self *fakeProvider) Send(_ context.Context, yield agent.Yield) (agent.Reply, error) {
	self.turn++

	time.Sleep(thinkingFor)

	for _, thought := range []string{
		"**Reading the file**\nThe path they gave is the one to start with.",
		"**Searching for the spinner**\nIt is drawn somewhere near the output layer.",
	} {
		if !yield(agent.Output{Kind: agent.ModelReasoningEvent, Text: thought}) ||
			!yield(agent.Output{Kind: agent.ModelReasoningEvent, Done: true}) {
			return agent.Reply{}, nil
		}

		time.Sleep(wordEvery)
	}

	for word := range strings.FieldsSeq("let me have a look at that for you") {
		if !yield(agent.Output{Kind: agent.ModelMessageEvent, Text: word + " "}) {
			return agent.Reply{}, nil
		}

		time.Sleep(wordEvery)
	}
	if !yield(agent.Output{Kind: agent.ModelMessageEvent, Done: true}) {
		return agent.Reply{}, nil
	}

	if self.turn > 1 {
		return agent.Reply{}, nil
	}

	return agent.Reply{Calls: []agent.ToolCall{
		{ID: "1", Name: "read", Arguments: `{"path":"main.go"}`},
		{ID: "2", Name: "grep", Arguments: `{"path":"spinner"}`},
		{ID: "3", Name: "write", Arguments: `{"path":"notes.md"}`},
	}}, nil
}

type fakeArgs struct {
	Path string `json:"path"`
}

func slowToolBuilder(name string) tool.Builder[fakeArgs] {
	return tool.Implement(
		tool.Definition{
			Name:        name,
			Description: "",
			Schema:      tool.Schema{tool.String("path", "file")},
		},
		func(args fakeArgs) (string, string) { return args.Path, "" },
	)
}

func buildSlowTool(builder tool.Builder[fakeArgs]) tool.Tool {
	return builder.Plain(func(context.Context, fakeArgs) (string, error) {
		time.Sleep(toolTakes)
		return "one\ntwo\nthree", nil
	})
}

func slowTool(name string) tool.Tool {
	return buildSlowTool(slowToolBuilder(name))
}

func slowReadTool(name string) tool.Tool {
	return buildSlowTool(slowToolBuilder(name).IsEmbarrassinglyParallel().ChangesNothing())
}

func failingTool(name string) tool.Tool {
	return tool.Implement(
		tool.Definition{
			Name:        name,
			Description: "",
			Schema:      tool.Schema{tool.String("path", "file")},
		},
		func(args fakeArgs) (string, string) { return args.Path, "" },
	).Plain(func(context.Context, fakeArgs) (string, error) {
		time.Sleep(toolTakes)
		return "", errors.New("permission denied\nnothing was written")
	})
}

func TestVisual(t *testing.T) {
	if os.Getenv("RIG") == "" {
		t.Skip("set RIG to watch it draw")
	}

	tools := []tool.Tool{slowReadTool("read"), slowReadTool("grep"), failingTool("write")}
	provider := &fakeProvider{}
	screen := output.New(os.Stdout)

	log, err := store.Create(t.TempDir(), store.Meta{Model: "fake"})
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = log.Close() }()

	held := &App{
		agent:    agent.New("", provider, tools),
		screen:   screen,
		recorder: record.New(log),
		mode:     caps.NewMode(caps.Read | caps.Write),
	}

	built, err := configFrom(t, "").BuildLayout(
		availableSegments("/tmp/somewhere", log.Name(), "fake", "medium", held),
	)
	if err != nil {
		t.Fatal(err)
	}

	held.barConfiguration = bar.NewConfiguration(nil, built)

	held.begin("")
}

type frameRecordingWriter struct {
	stream strings.Builder
	frames []string
}

func (self *frameRecordingWriter) Write(value []byte) (int, error) {
	_, _ = self.stream.Write(value)
	self.frames = append(self.frames, self.stream.String())
	return len(value), nil
}

func TestOrdinaryTabDrawsWhatItDrewBefore(t *testing.T) {
	pass := func() string {
		self := slashCommandFixture(t, caps.Read)
		var screenOutput strings.Builder
		self.screen = output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines)

		history := edit.NewHistory("", historyLimit)
		editor := edit.NewInput(history)
		editor.SetText("ordinary input")

		self.screen.Line("conversation remains in scrollback")
		self.show(editor)
		self.handleKeypressAndShowInput(editor, history, key.Key{Code: key.Rune, Value: '\t'})
		for _, value := range "after tab" {
			self.handleKeypressAndShowInput(editor, history, key.Key{Code: key.Rune, Value: value})
		}

		return screenOutput.String()
	}
	passes := map[string]func() string{"tab inserts four spaces": pass}

	compareWithGolden(t, "ordinary-tab", ".ansi", passes)
	compareWithGolden(t, "ordinary-tab", ".screen", shownPasses(t, passes))
}

func TestAPasteIsDrawnOnlyWhenItHasFinished(t *testing.T) {
	writer := &frameRecordingWriter{}
	self := slashCommandFixture(t, caps.Read)
	self.screen = output.NewTerminalOfSize(writer, replayColumns, replayLines)

	history := edit.NewHistory("", historyLimit)
	editor := edit.NewInput(history)
	self.show(editor)
	framesBeforePaste := len(writer.frames)

	self.handleKeypressAndShowInput(editor, history, key.Key{Code: key.PasteStart})
	for _, value := range "pasted text" {
		self.handleKeypressAndShowInput(editor, history, key.Key{Code: key.Rune, Value: value})
	}

	if drawn := len(writer.frames) - framesBeforePaste; drawn != 0 {
		t.Errorf("the input was drawn %d times while the paste arrived, want 0", drawn)
	}

	self.handleKeypressAndShowInput(editor, history, key.Key{Code: key.PasteEnd})

	visible := strings.Join(visibleScreen(t, writer.stream.String(), replayColumns), "\n")
	if !strings.Contains(visible, "pasted text") {
		t.Errorf("the finished paste was not drawn:\n%s", visible)
	}
}

type pasteStage int

const (
	pasteNotStarted pasteStage = iota
	pasteArriving
	pasteFinished
)

func TestAPasteDrawsWhatItDrewBefore(t *testing.T) {
	passes := map[string]func() string{
		"1 before the paste": func() string {
			return pasteStream(t, "pasted text", pasteNotStarted)
		},
		"2 while the paste arrives": func() string {
			return pasteStream(t, "pasted text", pasteArriving)
		},
		"3 after the paste": func() string {
			return pasteStream(t, "pasted text", pasteFinished)
		},
		"4 a paste of many lines": func() string {
			return pasteStream(t, "first line\nsecond line\nthird line", pasteFinished)
		},
		"5 an indented paste": func() string {
			return pasteStream(t, "    if isReady {\n        begin()\n    }", pasteFinished)
		},
		"6 a paste wider than the screen": func() string {
			return pasteStream(t, strings.Repeat("wide ", 40), pasteFinished)
		},
	}

	compareWithGolden(t, "paste", ".ansi", passes)
	compareWithGolden(t, "paste", ".screen", shownPasses(t, passes))
}

func pasteStream(t *testing.T, text string, stage pasteStage) string {
	t.Helper()

	self := slashCommandFixture(t, caps.Read)
	var screenOutput strings.Builder
	self.screen = output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines)

	history := edit.NewHistory("", historyLimit)
	editor := edit.NewInput(history)

	self.screen.Line("conversation remains in scrollback")
	self.show(editor)

	if stage == pasteNotStarted {
		return screenOutput.String()
	}

	self.handleKeypressAndShowInput(editor, history, key.Key{Code: key.PasteStart})
	for _, value := range text {
		if value == '\n' {
			self.handleKeypressAndShowInput(editor, history, key.Key{Code: key.Enter})
			continue
		}
		self.handleKeypressAndShowInput(editor, history, key.Key{Code: key.Rune, Value: value})
	}

	if stage == pasteFinished {
		self.handleKeypressAndShowInput(editor, history, key.Key{Code: key.PasteEnd})
	}

	return screenOutput.String()
}

func TestControlCDrawsWhatItDrewBefore(t *testing.T) {
	written := "a line worth keeping"
	controlC := key.Key{Code: key.Rune, Value: 'c', Mod: key.Ctrl}
	controlU := key.Key{Code: key.Rune, Value: 'u', Mod: key.Ctrl}
	bang := key.Key{Code: key.Rune, Value: '!'}

	passes := map[string]func() string{
		"1 a written line": func() string {
			return clearingStream(t, written)
		},
		"2 after one ctrl+c": func() string {
			return clearingStream(t, written, controlC)
		},
		"3 after two ctrl+cs": func() string {
			return clearingStream(t, written, controlC, controlC)
		},
		"4 a key between two ctrl+cs": func() string {
			return clearingStream(t, written, controlC, bang, controlC)
		},
		"5 after one ctrl+u": func() string {
			return clearingStream(t, written, controlU)
		},
	}

	compareWithGolden(t, "clearing", ".ansi", passes)
	compareWithGolden(t, "clearing", ".screen", shownPasses(t, passes))
}

func clearingStream(t *testing.T, written string, keypresses ...key.Key) string {
	t.Helper()

	self := slashCommandFixture(t, caps.Read)
	var screenOutput strings.Builder
	self.screen = output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines)

	history := edit.NewHistory("", historyLimit)
	editor := edit.NewInput(history)

	self.screen.Line("conversation remains in scrollback")
	self.show(editor)

	for _, value := range written {
		self.handleKeypressAndShowInput(editor, history, key.Key{Code: key.Rune, Value: value})
	}

	for _, keypress := range keypresses {
		self.handleKeypressAndShowInput(editor, history, keypress)
	}

	return screenOutput.String()
}

func TestHelpDuringReasoningIsDrawnInOneFrame(t *testing.T) {
	frames := helpDuringReasoningFrames(t)
	if len(frames) != 1 {
		t.Fatalf("help was drawn across %d frames, want 1", len(frames))
	}

	visible := strings.Join(visibleScreen(t, frames[0], replayColumns), "\n")
	if !strings.Contains(visible, "Commands:\n  /conf\n  /copy") {
		t.Errorf("help did not settle in its frame:\n%s", visible)
	}
	if strings.Contains(visible, "/help") {
		t.Errorf("accepted input remained in the settled frame:\n%s", visible)
	}
}

func helpDuringReasoningFrames(t *testing.T) []string {
	t.Helper()

	writer := &frameRecordingWriter{}
	self := slashCommandFixture(t, caps.Read)
	self.screen = output.NewTerminalOfSize(writer, replayColumns, replayLines)
	self.commands = fixtureCommandRegistry(t, slash.Command{
		Name: "help",
		Run: func(context slash.Context, _ slash.Arguments) error {
			context.Notice("Commands:\n  /conf\n  /copy")
			return nil
		},
	})
	self.currentTurn = Turn{Stream: testRunningTurnStream(), painter: self.newPainter(true)}

	history := edit.NewHistory("", historyLimit)
	editor := edit.NewInput(history)
	self.editor = editor
	editor.SetText("/help")
	self.show(editor)
	self.currentTurn.painter.DrawDelta(agent.Delta{Kind: agent.ModelReasoningEvent, Text: "thinking about it"})
	framesBeforeHelp := len(writer.frames)

	self.handleKeypressAndShowInput(editor, history, key.Key{Code: key.Enter})

	frames := slices.Clone(writer.frames[framesBeforeHelp:])
	self.currentTurn.painter.Close(dynamic.Cancelled)
	return frames
}

func shownHelpDuringReasoningFrames(t *testing.T) string {
	t.Helper()

	var shown strings.Builder
	for index, frame := range helpDuringReasoningFrames(t) {
		fmt.Fprintf(&shown, "--- frame %d ---\n%s\n", index+1, strings.Join(visibleScreen(t, frame, replayColumns), "\n"))
	}
	return strings.TrimSuffix(shown.String(), "\n")
}

type usageReport struct {
	windows []agent.UsageWindow
	err     error
	thenErr error
}

type scriptedUsageReporter struct {
	report   usageReport
	answered atomic.Int64
}

func (self *scriptedUsageReporter) IsAvailable() bool {
	return true
}

func (self *scriptedUsageReporter) UsageWindows(context.Context) ([]agent.UsageWindow, error) {
	defer self.answered.Add(1)

	if self.answered.Load() > 0 && self.report.thenErr != nil {
		return nil, self.report.thenErr
	}

	return self.report.windows, self.report.err
}

func meteredWindows(at time.Time) []agent.UsageWindow {
	return []agent.UsageWindow{
		{Duration: 7 * 24 * time.Hour, Percent: 6, ResetsAt: at.Add(6 * 24 * time.Hour)},
		{
			Duration: 5 * time.Hour,
			Percent:  4,
			ResetsAt: at.Add(4 * time.Hour),
			Scope:    "gpt-5.3-codex-spark",
		},
		{
			Duration: 7 * 24 * time.Hour,
			Percent:  2,
			ResetsAt: at.Add(6 * 24 * time.Hour),
			Scope:    "gpt-5.3-codex-spark",
		},
	}
}

func goldenUsagePass(
	t *testing.T, at time.Time, modelName string, report usageReport,
) func() string {
	t.Helper()

	var now atomic.Int64
	now.Store(at.UnixNano())

	reporter := &scriptedUsageReporter{report: report}
	readClock := func() time.Time { return time.Unix(0, now.Load()).UTC() }

	built, err := subUsage.New(reporter, "", modelName, readClock)(goldenSegmentOptions(""))
	if err != nil {
		t.Fatal(err)
	}

	return func() string {
		drawn := settleUsage(t, built, reporter, 1)

		if report.thenErr == nil {
			return drawn
		}

		now.Store(at.Add(time.Hour).UnixNano())

		return settleUsage(t, built, reporter, 2)
	}
}

func goldenUsageFromCache(
	t *testing.T, at time.Time, modelName string, windows []agent.UsageWindow,
) func() string {
	t.Helper()

	readClock := func() time.Time { return at }
	cachePath := filepath.Join(t.TempDir(), "usage", "codex.json")

	seeded := &scriptedUsageReporter{report: usageReport{windows: windows}}
	if _, err := usage.Shared(seeded, cachePath, time.Hour, readClock).UsageWindows(t.Context()); err != nil {
		t.Fatal(err)
	}

	silent := &scriptedUsageReporter{}

	built, err := subUsage.New(silent, cachePath, modelName, readClock)(goldenSegmentOptions(""))
	if err != nil {
		t.Fatal(err)
	}

	return func() string { return built.Render(segment.Context{}) }
}

func settleUsage(
	t *testing.T, built segment.Segment, reporter *scriptedUsageReporter, answers int64,
) string {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	drawn := built.Render(segment.Context{})

	for {
		if time.Now().After(deadline) {
			t.Fatalf("the usage segment never settled, last drew %q", drawn)
		}

		time.Sleep(time.Millisecond)

		redrawn := built.Render(segment.Context{})
		if reporter.answered.Load() >= answers && redrawn == drawn {
			return redrawn
		}

		drawn = redrawn
	}
}

func TestEveryWayOfStoppingATurnSaysWhy(t *testing.T) {
	tests := map[string]struct {
		stopTurn func(*App)
		want     string
	}{
		"escape": {
			stopTurn: func(self *App) { self.cancelTurn(escapeReason) },
			want:     "the user pressed escape",
		},
		"another message": {
			stopTurn: func(self *App) { self.replaceTurn("do this instead") },
			want:     "the user sent another message",
		},
		"a capability change": {
			stopTurn: func(self *App) { self.toggleCap(caps.Git) },
			want:     "the user changed what the harness is allowed to do",
		},
		"leaving the session": {
			stopTurn: func(self *App) {
				if err := self.requestTransition(cycle.Transition{Kind: cycle.NewSession}); err != nil {
					t.Fatal(err)
				}
			},
			want: "the session is being closed",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			var cause error

			self := &App{
				screen:   output.New(&bytes.Buffer{}),
				mode:     caps.NewMode(caps.Read),
				recorder: record.New(testLog(t)),
			}
			self.currentTurn = Turn{
				Stream:  testTurnStream(nil, func(err error) { cause = err }, turn.State{Running: true}),
				painter: self.newPainter(true),
			}

			test.stopTurn(self)

			ctx, cancel := context.WithCancelCause(t.Context())
			cancel(cause)

			if got := stop.Sentence(ctx); got != test.want {
				t.Errorf("got %q, want the turn stopped because %q", got, test.want)
			}
		})
	}
}

type notingProvider struct {
	quietProvider

	mutex sync.Mutex
	notes []string
}

func (self *notingProvider) AddUserMessage(text string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.notes = append(self.notes, text)
}

func (self *notingProvider) told() []string {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return slices.Clone(self.notes)
}

func TestTheHarnessAsksForATitleOnlyOnceTheModelHasAnsweredWithoutGivingOne(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	backend := &notingProvider{}
	self.agent = agent.New("", backend, []tool.Tool{title.New()})

	if note := self.titleNote(); note != "" {
		t.Errorf("expected the opening turn to be left alone, got %q", note)
	}

	self.events = append(self.events, agent.Event{Kind: agent.ModelMessageEvent, Text: "done"})
	if note := self.titleNote(); !strings.Contains(note, title.Name) {
		t.Errorf("expected an unanswered session to be asked for a title, got %q", note)
	}

	completeTurn(self)
	if !slices.ContainsFunc(backend.told(), func(note string) bool {
		return strings.Contains(note, "has no title")
	}) {
		t.Errorf("the model was told %q", backend.told())
	}

	self.events = append(self.events, agent.Event{
		Kind:  agent.StateChangeEvent,
		Name:  agent.TitleStateKey,
		State: json.RawMessage(`{"title":"fix the picker clipping"}`),
	})
	if note := self.titleNote(); note != "" {
		t.Errorf("expected a titled session to be left alone, got %q", note)
	}
}

func TestASessionWithoutTheTitleToolIsNeverAskedForATitle(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	self.agent = agent.NewWithEnabledTools("", quietProvider{}, []tool.Tool{title.New()}, nil)
	self.events = append(self.events, agent.Event{Kind: agent.ModelMessageEvent, Text: "done"})

	if note := self.titleNote(); note != "" {
		t.Errorf("expected a session that cannot title itself to be left alone, got %q", note)
	}
}

func TestTheSessionTitleReachesTheTerminalLiveAndOnResume(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)

	self.restore(&store.Session{Events: []agent.Event{
		{Kind: agent.UserMessageEvent, Text: "have a look at the picker"},
		{
			Kind:  agent.StateChangeEvent,
			Name:  agent.TitleStateKey,
			State: json.RawMessage(`{"title":"fix the picker clipping"}`),
		},
	}})

	if got := self.terminal.GetSessionTitle(); got != "fix the picker clipping" {
		t.Errorf("a resumed session went by %q", got)
	}

	self.currentTurn = Turn{painter: self.newPainter(true)}
	self.recordEvent(agent.Event{
		Kind:  agent.StateChangeEvent,
		Name:  agent.TitleStateKey,
		State: json.RawMessage(`{"title":"give sessions a title"}`),
	})

	if got := self.terminal.GetSessionTitle(); got != "give sessions a title" {
		t.Errorf("a retitled session went by %q", got)
	}
}

type plainTurnProvider struct {
	items []json.RawMessage
}

func (self *plainTurnProvider) Configure(string, []tool.Definition)   {}
func (self *plainTurnProvider) AddToolResults([]agent.ToolCallResult) {}
func (self *plainTurnProvider) Dump() []json.RawMessage               { return self.items }
func (self *plainTurnProvider) Load(items []json.RawMessage)          { self.items = items }

func (self *plainTurnProvider) AddUserMessage(text string) {
	self.items = append(self.items, fmt.Appendf(nil, `{"role":"user","content":%q}`, text))
}

func (self *plainTurnProvider) Send(_ context.Context, yield agent.Yield) (agent.Reply, error) {
	if !yield(agent.Output{Kind: agent.ModelMessageEvent, Text: "Said it."}) {
		return agent.Reply{}, nil
	}
	if !yield(agent.Output{Kind: agent.ModelMessageEvent, Done: true}) {
		return agent.Reply{}, nil
	}

	self.items = append(self.items, json.RawMessage(`{"role":"assistant","content":"Said it."}`))

	return agent.Reply{}, nil
}

func TestTheAppWritesAndResumesASessionOnItsOwn(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{
		Model:        "claude-opus-5",
		Provider:     "anthropic",
		Effort:       "medium",
		SystemPrompt: sessionGoldenSystemPrompt,
	})
	if err != nil {
		t.Fatal(err)
	}

	var screenOutput bytes.Buffer
	self := &App{
		agent:    agent.New(sessionGoldenSystemPrompt, &plainTurnProvider{}, nil),
		screen:   output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines),
		recorder: record.New(log),
		mode:     caps.NewMode(caps.All()),
	}

	self.begin("say something")

	sessionName := log.Name()
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	transcript, err := os.ReadFile(filepath.Join(directory, sessionName, "chat.md")) //nolint:gosec // the path is the test's own session directory
	if err != nil {
		t.Fatal(err)
	}

	compareScenarioGolden(t, "app-plain-turn.jsonl", canonicalSessionJournal(t, directory, sessionName))
	compareScenarioGolden(
		t, "app-plain-turn.transcript", canonicalSessionTranscript(string(transcript), sessionName),
	)

	resumeAppPlainTurn(t, directory, sessionName)
}

func resumeAppPlainTurn(t *testing.T, directory string, sessionName string) {
	t.Helper()

	storedSession, err := store.Read(directory, sessionName)
	if err != nil {
		t.Fatal(err)
	}

	resumedCaps, err := sessions.OpeningCaps(caps.All(), false, storedSession)
	if err != nil {
		t.Fatal(err)
	}

	log, err := store.Open(directory, sessionName)
	if err != nil {
		t.Fatal(err)
	}

	var screenOutput bytes.Buffer
	self := &App{
		agent:    agent.New(storedSession.Meta.SystemPrompt, &plainTurnProvider{}, nil),
		screen:   output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines),
		recorder: record.New(log),
		mode:     caps.NewMode(resumedCaps),
	}
	self.restore(storedSession)

	self.begin("carry on")

	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	transcript, err := os.ReadFile(filepath.Join(directory, sessionName, "chat.md")) //nolint:gosec // the path is the test's own session directory
	if err != nil {
		t.Fatal(err)
	}

	compareScenarioGolden(t, "app-plain-resume.jsonl", canonicalSessionJournal(t, directory, sessionName))
	compareScenarioGolden(
		t, "app-plain-resume.transcript", canonicalSessionTranscript(string(transcript), sessionName),
	)
}

func TestTheStartupUsageProbeLeavesNoSessionBehind(t *testing.T) {
	var probes atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		probes.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"usage":{"rolling":{"status":"ok","percent":6}}}`)
	}))
	defer server.Close()

	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{Model: "model", Provider: "opencode-go"})
	if err != nil {
		t.Fatal(err)
	}

	client, err := opencodego.New(server.URL, "token", "model", "medium", 1024)
	if err != nil {
		t.Fatal(err)
	}
	client.UsageURL = server.URL
	client.ObserveHTTP(log.Observer())

	cachePath := filepath.Join(t.TempDir(), "usage", "opencode-go.json")
	built, err := subUsage.New(client, cachePath, "model", time.Now)(goldenSegmentOptions(""))
	if err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for probes.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the usage probe was never made")
		}
		built.Render(segment.Context{})
		time.Sleep(time.Millisecond)
	}

	if log.IsPersisted() {
		t.Error("the usage probe wrote the session down before anything was said in it")
	}
	if warnings := log.TakeWarnings(); len(warnings) != 0 {
		t.Errorf("expected no recorder warnings, got %v", warnings)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		t.Errorf("the usage probe left %s behind", entry.Name())
	}
}

func TestNoRecordedRequestMarksAThoughtForCaching(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("testdata", "output", "*.requests.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no recorded requests")
	}

	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			requests, err := os.ReadFile(path) //nolint:gosec // fixed testdata path
			if err != nil {
				t.Fatal(err)
			}

			for line := range strings.SplitSeq(strings.TrimSpace(string(requests)), "\n") {
				var request any
				if err := json.Unmarshal([]byte(line), &request); err != nil {
					t.Fatal(err)
				}

				for _, thought := range markedThoughts(request) {
					t.Errorf("expected no cache breakpoint on a thought, got %v", thought)
				}
			}
		})
	}
}

func markedThoughts(node any) []map[string]any {
	var found []map[string]any

	switch node := node.(type) {
	case map[string]any:
		_, isMarked := node["cache_control"]
		if isMarked && (node["type"] == "thinking" || node["type"] == "redacted_thinking") {
			found = append(found, node)
		}
		for _, child := range node {
			found = append(found, markedThoughts(child)...)
		}
	case []any:
		for _, child := range node {
			found = append(found, markedThoughts(child)...)
		}
	}

	return found
}

func TestEverySessionGoldenOpensWithTheModeItIsIn(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("testdata", "output", "*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no session journals")
	}

	for _, path := range paths {
		if strings.HasSuffix(path, ".requests.jsonl") {
			continue
		}

		t.Run(filepath.Base(path), func(t *testing.T) {
			journal, err := os.ReadFile(path) //nolint:gosec // fixed testdata path
			if err != nil {
				t.Fatal(err)
			}

			for line := range strings.SplitSeq(strings.TrimSpace(string(journal)), "\n") {
				var record struct {
					Kind  session.Kind `json:"kind"`
					Event *agent.Event `json:"event"`
				}
				if err := json.Unmarshal([]byte(line), &record); err != nil {
					t.Fatal(err)
				}
				if record.Event == nil {
					continue
				}

				if record.Event.Kind != caps.ModeChange {
					t.Fatalf("expected the mode to be recorded first, got %q", record.Event.Kind)
				}

				return
			}

			t.Fatal("expected the session to record the mode it opened in")
		})
	}
}

func TestAModeChangeIsNotLostWhenItsQueuedTurnIsDropped(t *testing.T) {
	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = log.Close() }()

	provider := &refusingOnceProvider{}
	self := &App{
		agent:    agent.New("", provider, nil),
		screen:   output.New(&bytes.Buffer{}),
		recorder: record.New(log),
		mode:     caps.NewMode(caps.Read | caps.Write),
	}

	takeTestTurn := func(message string) {
		self.start(message)
		for report := range self.currentTurn.Events() {
			self.takeTurn(report)
		}
		self.finish()
	}

	self.start("first")
	self.toggleCap(caps.Write)
	self.cancelTurn(escapeReason)
	for report := range self.currentTurn.Events() {
		self.takeTurn(report)
	}
	self.finish()

	if self.currentTurn.Running() {
		t.Fatal("expected the dropped mode change to have started no turn of its own")
	}

	takeTestTurn("second")

	if said := countModeNotes(provider.messages, nowReadOnlyNote); said != 1 {
		t.Errorf("expected the dropped mode change to be said once, said %d in %q", said, provider.messages)
	}
}

func TestAModeChangeIsNotLostWhenAMessageReplacesItsTurn(t *testing.T) {
	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = log.Close() }()

	provider := &refusingOnceProvider{}
	self := &App{
		agent:    agent.New("", provider, nil),
		screen:   output.New(&bytes.Buffer{}),
		recorder: record.New(log),
		mode:     caps.NewMode(caps.Read | caps.Write),
	}

	self.start("first")
	self.toggleCap(caps.Write)
	self.replaceTurn("second")

	for self.currentTurn.Running() {
		for report := range self.currentTurn.Events() {
			self.takeTurn(report)
		}
		self.finish()
	}

	if said := countModeNotes(provider.messages, nowReadOnlyNote); said != 1 {
		t.Errorf("expected the replaced mode change to be said once, said %d in %q", said, provider.messages)
	}
}
