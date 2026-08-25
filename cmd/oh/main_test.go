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
	"crdx.org/io/cmd/oh/location"
	"crdx.org/io/cmd/oh/metrics"
	"crdx.org/io/cmd/oh/model"
	"crdx.org/io/cmd/oh/output"
	"crdx.org/io/cmd/oh/painter"
	"crdx.org/io/cmd/oh/picker"
	"crdx.org/io/cmd/oh/prompt"
	"crdx.org/io/cmd/oh/record"
	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/segment/activeModel"
	"crdx.org/io/cmd/oh/segment/activitySpinner"
	"crdx.org/io/cmd/oh/segment/contextUsage"
	"crdx.org/io/cmd/oh/segment/gitBranch"
	"crdx.org/io/cmd/oh/segment/lastTps"
	"crdx.org/io/cmd/oh/segment/localTime"
	"crdx.org/io/cmd/oh/segment/modeToggle"
	"crdx.org/io/cmd/oh/segment/scrollOverflow"
	"crdx.org/io/cmd/oh/segment/sessionName"
	"crdx.org/io/cmd/oh/segment/subUsage"
	"crdx.org/io/cmd/oh/segment/turnCount"
	"crdx.org/io/cmd/oh/segment/turnElapsed"
	"crdx.org/io/cmd/oh/segment/workingDirectory"
	"crdx.org/io/cmd/oh/sessions"
	"crdx.org/io/cmd/oh/shell"
	"crdx.org/io/cmd/oh/skill"
	"crdx.org/io/cmd/oh/slash"
	"crdx.org/io/cmd/oh/snippets"
	"crdx.org/io/cmd/oh/startup"
	"crdx.org/io/cmd/oh/store"
	"crdx.org/io/cmd/oh/store/transcript"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/oh/turn"
	"crdx.org/io/cmd/oh/workspace"
	"crdx.org/io/internal/file"
	"crdx.org/io/internal/sandbox"
	"crdx.org/io/internal/sim"
	"crdx.org/io/internal/util/pathutil"
	"crdx.org/io/provider/anthropic"
	"crdx.org/io/provider/chat"
	"crdx.org/io/provider/codex"
	"crdx.org/io/session"
	"crdx.org/io/tool"
	"crdx.org/io/tool/middleware/truncate"
	"crdx.org/io/toolbox"
	"crdx.org/io/toolbox/bash"
	"crdx.org/io/toolbox/notify"
	"crdx.org/io/toolbox/web"
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
	self := &App{currentTurn: Turn{Stream: testTurnStream(nil, func() {}, turn.State{Running: true})}}
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
	self := &App{
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
	self := &App{
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
	self := &App{screen: output.New(&bytes.Buffer{})}
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
			self := &App{
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

	self := &App{
		agent:    agent.New("", quietProvider{}, nil),
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

	self := &App{
		agent:    agent.New("", quietProvider{}, nil),
		screen:   output.New(&bytes.Buffer{}),
		recorder: record.New(log),
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

func TestCompletionProtocolMatchesTheGolden(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	cachePath := location.GetModelCachePath(os.Getenv(backend.EndpointVariable) != "")
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o700); err != nil { //nolint:gosec // the path is the test's own state directory
		t.Fatal(err)
	}
	cache := []byte(`{"version":1,"providers":{"codex":{"models":[{"id":"gpt-5","efforts":["low","high"],"output":128000}]},"anthropic":{"models":[{"id":"claude-sonnet-5","efforts":["none","high"],"output":128000}]}}}`)
	if err := os.WriteFile(cachePath, cache, 0o600); err != nil { //nolint:gosec // the path is the test's own state directory
		t.Fatal(err)
	}

	directory := t.TempDir()
	writeStoredSession(t, directory, "older-badger", "2024-01-01T00:00:00Z")
	writeStoredSession(t, directory, "newer-jaguar", "2025-01-01T00:00:00Z")
	sources := cli.Sources{ModelCachePath: cachePath, SessionsDir: directory, ToolNames: completableToolNames}

	requests := []struct {
		name string
		args []string
	}{
		{name: "options", args: []string{"--complete", "option", ""}},
		{name: "models", args: []string{"--complete", "model", "sonnet"}},
		{name: "efforts", args: []string{"--complete", "effort", "sonnet@"}},
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

func TestAHarnessNoticeIsDrawnTheSameLiveAndReplayed(t *testing.T) {
	const notice = "Background processes killed (tmux: server → bash → sleep)"

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

	self.notifyStopped(notice)

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
	self.notifyStopped("Background processes killed (bash → sleep)")

	unansweredCall := agent.Event{Kind: agent.ToolCallRequestEvent, ID: "3", Name: "read", FallbackRendering: agent.FallbackRendering{Subject: "left.go"}}
	self.events = append(self.events, unansweredCall)
	livePainter.DrawEvent(unansweredCall)
	livePainter.Close(dynamic.Cancelled)
	self.screen.End()

	var replayOutput bytes.Buffer
	self.screen = output.New(&replayOutput)
	self.replay()

	plain := style.Plain(live.String())

	for _, want := range []string{"Check", "Looking at the file.", "Need care.", "first answer.", "one.go", "Done.", "Background processes killed"} {
		if !strings.Contains(plain, want) {
			t.Errorf("expected %q on the screen, got %q", want, plain)
		}
	}
	if !strings.Contains(plain, "Need care.\n\nThe first") {
		t.Errorf("expected a blank line between reasoning and the answer, got %q", plain)
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
		agent:              agent.New("", backend, nil),
		screen:             output.New(screenOutput),
		recorder:           record.New(log),
		mode:               caps.NewMode(caps.Read | caps.Write),
		getOnWithItMessage: builtInConfig(t).GetOnWithItMessage,
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
	self.currentTurn.Interrupt()

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
	if !strings.Contains(plain, "stream failed") {
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
		"banner":            {".ansi", ".screen"},
		"completion":        {".txt"},
		"context":           {".prompt"},
		"inputblock":        {".ansi", ".screen"},
		"lifecycle":         {".ansi", ".screen"},
		"mermaid-streaming": {".screen"},
		"mode-takeback":     {".ansi", ".screen"},
		"new-session":       {".txt"},
		"resume-mode":       {".ansi"},
		"running":           {".ansi", ".screen"},
		"segments":          {".ansi", ".screen"},
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
	os.Exit(testingMain.Run())
}

func TestHomeMountIsReadableByFileTools(t *testing.T) {
	workspaceRoot, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workspaceRoot.Close() }()

	files := file.New(workspaceRoot, func(string) error { return file.ErrReadOnly })
	home := t.TempDir()
	path := filepath.Join(home, "reference")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	homeRoot, err := mountHomeDir(files, home, caps.NewMode(caps.Read))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = homeRoot.Close() }()

	resolvedRoot, name, err := files.Resolve(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := resolvedRoot.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Errorf("got %q, want hello", data)
	}
}

func TestTmpMountIsWritableWithoutAShell(t *testing.T) {
	workspaceRoot, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workspaceRoot.Close() }()

	files := file.New(workspaceRoot, func(string) error { return file.ErrReadOnly })
	tmp := t.TempDir()
	tmpRoot, err := mountTmpDir(files, tmp)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tmpRoot.Close() }()

	resolvedRoot, name, err := files.Resolve("/tmp/proof")
	if err != nil {
		t.Fatal(err)
	}
	if err := resolvedRoot.WriteFile(name, []byte("written"), 0o600); err != nil {
		t.Fatalf("tmp was not writable: %v", err)
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

	resumedHarness := &App{mode: caps.NewResumedMode(restoredCaps)}
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
	if len(self.pendingModeChanges) != 1 {
		t.Fatalf("expected the change to be shown, got %v", self.pendingModeChanges)
	}

	self.toggleCap(caps.Git)
	if len(self.pendingModeChanges) != 0 {
		t.Errorf("expected the change to be taken back, got %v", self.pendingModeChanges)
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

	shown, said := caps.ModeNotice(self.pendingModeChanges[1])
	if !said {
		t.Fatal("expected the second change to say something")
	}

	self.toggleCap(caps.Git)
	if again, _ := caps.ModeNotice(self.pendingModeChanges[0]); again != shown {
		t.Errorf("expected %q, got %q", shown, again)
	}

	self.settleMode()
	want := caps.Read | caps.Shell
	if got, _ := caps.LastRecordedMode(recordedModes(t, self, directory)); got != want {
		t.Errorf("expected %s, got %s", want.Flags(), got.Flags())
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
		"2 invalid extension": func() string {
			return mermaidStreamingScreen(t, "```mermaid\ngraph LR\nA --> B", "\nB -->")
		},
		"3 invalid after redraw": func() string {
			return mermaidStreamingRedrawnScreen(t)
		},
		"4 next valid prefix": func() string {
			return mermaidStreamingScreen(t, "```mermaid\ngraph LR\nA --> B", "\nB -->", " C")
		},
		"5 completed invalid diagram": func() string {
			return completedInvalidMermaidScreen(t)
		},
	})
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

func TestModelListDispatchRunsThroughTheBinary(t *testing.T) {
	binary := buildTestBinary(t)
	stateDirectory := t.TempDir()
	cachePath := filepath.Join(stateDirectory, "org.crdx", "oh", "models.json")
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o700); err != nil {
		t.Fatal(err)
	}
	cache := []byte(`{"version":1,"providers":{"codex":{"models":[{"id":"gpt-cli","efforts":["high"],"output":128000}]}}}`)
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
		"effort fallback":         resolveNearestEfforts,
		"fork model globs":        func() string { return resolveForkedSessionGlobs(t) },
		"new session model globs": func() string { return resolveNewSessionGlobs(t) },
	})
}

func newSessionFixtureChoices() []model.Choice {
	return []model.Choice{
		{Provider: "anthropic", Model: "claude-opus-4-5", EffortLevels: []string{"low", "high"}},
		{Provider: "anthropic", Model: "claude-opus-5", EffortLevels: []string{"medium", "max"}},
		{Provider: "anthropic", Model: "claude-sonnet-4-5", EffortLevels: []string{"medium"}},
		{Provider: "codex", Model: "gpt-5.6-sol", EffortLevels: []string{"none", "high"}},
	}
}

func resolveForkedSessionGlobs(t *testing.T) string {
	t.Helper()

	choices := newSessionFixtureChoices()
	var written strings.Builder

	for _, glob := range []string{"", "opus-5", "opus-5@max", "nope"} {
		transition, err := forkedSessionTransition(glob, "medium", choices, "able-dolphin")
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
		transition, err := newSessionTransition("/workspace", glob, "medium", choices)
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

	data := []byte(`{"version":1,"providers":{"codex":{"models":[{"id":"gpt-5.6-sol","efforts":["none","high"],"output":128000}]},"opencode-go":{"models":[{"id":"deepseek-v4-pro","efforts":["high","max"],"output":128000}]},"anthropic":{"models":[{"id":"claude-opus-5","efforts":["high","max"],"output":128000}]}}}`)
	if err := os.WriteFile(path, data, 0o600); err != nil { //nolint:gosec // the path is the test's own state directory
		t.Fatal(err)
	}

	return path
}

type Painter = painter.Picasso

func newTestPainter(screen *output.Screen, isRunning bool) *Painter {
	return painter.New(screen, isRunning, nil, "")
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

	for i, prompt := range prompts {
		meta := fmt.Appendf(nil, `{"workspaceDir":"/home/alice/proj/%d"}`, i)
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
	case errors.Is(err, picker.ErrCancelled):
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
		CurrentCaps:  caps.Read | caps.Write | caps.Git | caps.Shell | caps.Background,
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

	turnSoFar = 9 * time.Second

	workspaceMarker   = "/workspace"
	lifecycleScenario = "success@rxw.jsonl"
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
				"wide":       func() string { return replayAtWidth(t, entries, replayColumns) },
				"narrow":     func() string { return replayAtWidth(t, entries, narrowColumns) },
				"tiny":       func() string { return replayAtWidth(t, entries, tinyColumns) },
				"unsized":    func() string { return replayAtWidth(t, entries, noColumns) },
				"one column": func() string { return replayAtWidth(t, entries, oneColumn) },
				"streamed":   func() string { return streamIntoBuffer(t, entries) },
				"plain":      func() string { return replayPlainly(t, entries) },
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
		passes["released and kept"+screen.name] = func() string {
			return replayThenRelease(t, screen.open, entries, true)
		}
		passes["released and unused"+screen.name] = func() string {
			return replayThenRelease(t, screen.open, entries, false)
		}
	}

	compareWithGolden(t, "lifecycle", ".ansi", passes)
	compareWithGolden(t, "lifecycle", ".screen", shownPasses(t, onATerminal(passes)))
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
		fmt.Fprintf(&drawn, "=== %s ===\n%s\n", pass, visibleEscapes(passes[pass]()))
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
	processes := sandbox.NewProcesses(false)
	t.Cleanup(func() { _, _ = processes.Disable() })

	tools := toolbox.Rummage(files, file.NewSnapshots())
	tools = append(
		tools,
		bash.New(
			files,
			func(context.Context) (sandbox.Policy, error) { return sandbox.Policy{}, nil },
			processes,
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

func streamIntoBuffer(t *testing.T, entries []replayEntry) string {
	t.Helper()

	rig := newReplayRig(t, replayColumns)
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

func visibleEscapes(stream string) string {
	var out strings.Builder

	for _, character := range stream {
		switch {
		case character == '\n':
			out.WriteByte('\n')
		case character == '\\':
			out.WriteString(`\\`)
		case character == '\x1b':
			out.WriteString(`\e`)
		case character == '\r':
			out.WriteString(`\r`)
		case character == '\t':
			out.WriteString(`\t`)
		case character < ' ' || character == 0x7f:
			fmt.Fprintf(&out, `\x%02X`, character)
		default:
			out.WriteRune(character)
		}
	}

	return out.String()
}

func TestALiveTurnLeavesTheSameScreenAsAReplayOfIt(t *testing.T) {
	for _, journal := range everyJournal(t) {
		t.Run(journal.name, func(t *testing.T) {
			entries := readJournal(t, journal.path)

			replayed := visibleScreen(t, replayAtWidth(t, entries, replayColumns), replayColumns)
			live := visibleScreen(t, streamIntoBuffer(t, entries), replayColumns)

			if !slices.Equal(replayed, live) {
				t.Errorf(
					"a live turn and a replay of it left different screens\n--- replayed ---\n%s\n--- live ---\n%s",
					strings.Join(replayed, "\n"), strings.Join(live, "\n"),
				)
			}
		})
	}
}

func TestTheBannerDrawsWhatItDrewBefore(t *testing.T) {
	passes := map[string]func() string{}

	for _, flags := range []string{"", "r", "rw", "rx", "rxw", "rxwb", "rxwbg", "rxwbgs", "rgb", "rs"} {
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

					held := &App{mode: caps.NewMode(grantedCaps)}
					held.currentTurn.Stream = testTimedTurnStream(isRunning, time.Now().Add(-turnSoFar), time.Now())
					held.currentTurn.spinnerFrame = 2

					built := goldenBarLayout(t, held)

					return renderBar(built, segment.BottomLeft, edit.Frame{})
				})
			}
		}
	}

	for name, inputTokens := range map[string]int{
		"context usage low":  5000,
		"context usage high": 182_000,
	} {
		passes[name] = func() string {
			held := &App{
				mode:    caps.NewMode(caps.All()),
				metrics: metrics.New(200_000),
				events: []agent.Event{{
					Kind:  agent.ModelMessageEvent,
					Usage: &agent.Usage{InputTokens: inputTokens},
				}},
			}
			held.metrics.Restore(held.events)

			built := goldenBarLayout(t, held)

			return renderBar(built, segment.BottomLeft, edit.Frame{})
		}
	}

	passes["the startup line"] = func() string {
		return startup.RenderBanner(1500*time.Microsecond, false, startup.Info{
			Session: "brave-otter",
			ContextFiles: []startup.File{
				{Name: "SYSTEM.md", Bytes: 740},
				{Name: "AGENTS.md", Bytes: 3 * 1024},
			},
			ProjectSkills: 3,
			GlobalSkills:  1,
			Snippets:      2,
			ToolBytes:     614,
		})
	}

	compareWithGolden(t, "banner", ".ansi", passes)
	compareWithGolden(t, "banner", ".screen", shownPasses(t, passes))
}

func TestTheInputBlockDrawsWhatItDrewBefore(t *testing.T) {
	frames := map[string]edit.Frame{
		"one row": {
			Rows: []string{"> what is the weather"}, Row: 0, Column: 21,
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

					held := &App{mode: caps.NewMode(caps.All())}
					held.currentTurn.Stream = testTimedTurnStream(true, time.Now().Add(-turnSoFar), time.Time{})
					held.currentTurn.spinnerFrame = 2

					built := goldenBarLayout(t, held)

					held.segmentLayout = built

					block := input.Block{
						Top: input.Ruler{
							Left:   held.bar(segment.TopLeft, frame),
							Center: held.bar(segment.TopCenter, frame),
							Right:  held.bar(segment.TopRight, frame),
						},
						Input: frame,
						Bottom: input.Ruler{
							Left:   held.bar(segment.BottomLeft, frame),
							Center: held.bar(segment.BottomCenter, frame),
							Right:  held.bar(segment.BottomRight, frame),
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

type screen struct {
	t           *testing.T
	rows        [][]rune
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
			size := 1
			for size < len(stream)-at && stream[at+size]&0xc0 == 0x80 {
				size++
			}

			self.put([]rune(stream[at : at+size])[0])
			at += size
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
		return skipUntilStringTerminator(stream, at)
	default:
		self.t.Fatalf("the screen was sent an escape it does not know: %q", stream[at:min(at+8, len(stream))])
		return len(stream)
	}
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
			row[at] = ' '
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

func (self *screen) put(value rune) {
	if self.column >= self.columns {
		if !self.isWrapping {
			return
		}

		self.row++
		self.column = 0
	}

	for len(self.rows) <= self.row {
		self.rows = append(self.rows, nil)
	}

	row := self.rows[self.row]
	for len(row) <= self.column {
		row = append(row, ' ')
	}

	row[self.column] = value
	self.rows[self.row] = row
	self.column++
}

func (self *screen) text() []string {
	lines := make([]string, 0, len(self.rows))

	for _, row := range self.rows {
		lines = append(lines, strings.TrimRight(string(row), " "))
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
		Sources:            harness.getBarSources(),
	})
}

func renderBar(layout segment.Layout, position segment.Position, frame edit.Frame) string {
	return bar.Render(layout, position, segment.Context{
		HiddenLinesAbove: frame.HiddenLinesAbove,
		HiddenLinesBelow: frame.HiddenLinesBelow,
	})
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
			{ segment = "activity-spinner", idle = "✧·", frames = ["✦·", "·✦", "·✧", "✧·"], rate = "125ms" },
			{ segment = "turn-elapsed" },
			{ segment = "mode-toggle" },
			{ segment = "working-directory" },
			{ segment = "git-branch" },
			{ segment = "active-model" },
			{ segment = "context-usage" },
		]
		center = []
		right = [
			{ segment = "turn-count" },
			{ segment = "last-tps" },
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

func TestEverySegmentDrawsItsRepresentativeStates(t *testing.T) {
	at := time.Date(2026, time.August, 23, 14, 32, 9, 0, time.UTC)
	spinnerOptions := `
		idle = "✧·"
		frames = ["✦·", "·✦"]
		rate = "125ms"
	`

	passes := map[string]func() string{
		"active-model / medium effort": goldenSegmentPass(
			t,
			activeModel.New("gpt-5.6-sol", "medium"),
			"",
			segment.Context{},
		),
		"activity-spinner / idle": goldenSegmentPass(
			t,
			activitySpinner.New(func() (bool, int) { return false, 0 }),
			spinnerOptions,
			segment.Context{},
		),
		"activity-spinner / running first frame": goldenSegmentPass(
			t,
			activitySpinner.New(func() (bool, int) { return true, 0 }),
			spinnerOptions,
			segment.Context{},
		),
		"activity-spinner / running second frame": goldenSegmentPass(
			t,
			activitySpinner.New(func() (bool, int) { return true, 1 }),
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
		"last-tps / a fast turn": goldenSegmentPass(
			t,
			lastTps.New(func() (float64, bool) { return 42.4, true }, func() bool { return false }),
			"",
			segment.Context{},
		),
		"last-tps / a slow turn": goldenSegmentPass(
			t,
			lastTps.New(func() (float64, bool) { return 4.25, true }, func() bool { return false }),
			"",
			segment.Context{},
		),
		"last-tps / running turn": goldenSegmentPass(
			t,
			lastTps.New(func() (float64, bool) { return 42.4, true }, func() bool { return true }),
			"",
			segment.Context{},
		),
		"last-tps / unknown while idle": goldenSegmentPass(
			t,
			lastTps.New(func() (float64, bool) { return 0, false }, func() bool { return false }),
			"",
			segment.Context{},
		),
		"last-tps / unknown while running": goldenSegmentPass(
			t,
			lastTps.New(func() (float64, bool) { return 0, false }, func() bool { return true }),
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
			subUsage.New(nil, func() time.Time { return at }),
			"",
			segment.Context{},
		),
		"turn-elapsed / completed": goldenSegmentPass(
			t,
			turnElapsed.New(func() (bool, time.Duration, bool) { return false, 69 * time.Second, true }),
			"",
			segment.Context{},
		),
		"turn-elapsed / running": goldenSegmentPass(
			t,
			turnElapsed.New(func() (bool, time.Duration, bool) { return true, 69 * time.Second, true }),
			"",
			segment.Context{},
		),
		"turn-elapsed / unknown": goldenSegmentPass(
			t,
			turnElapsed.New(func() (bool, time.Duration, bool) { return false, 0, false }),
			"",
			segment.Context{},
		),
		"working-directory": goldenSegmentPass(
			t,
			workingDirectory.New("/workspace/project"),
			"",
			segment.Context{},
		),
	}

	compareWithGolden(t, "segments", ".ansi", passes)
	compareWithGolden(t, "segments", ".screen", shownPasses(t, passes))
}

func writeStoredSession(t *testing.T, directory string, name string, started string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Join(directory, name), 0o700); err != nil {
		t.Fatal(err)
	}

	head := fmt.Sprintf(`{"kind":"head","time":%q,"id":%q,"name":%q}`+"\n", started, name, name)
	if err := os.WriteFile(filepath.Join(directory, name, "session.jsonl"), []byte(head), 0o600); err != nil {
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
}

func TestChoosingWithoutStoredSessionsFails(t *testing.T) {
	if _, err := sessions.Choose(t.TempDir(), nil, nil); err == nil {
		t.Error("expected an empty session list to fail")
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

	_, err := sessions.Choose(directory, nil, nil)
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
	writeStoredSession(t, directory, "able-dolphin", "2026-08-01T00:00:00Z")

	_, err := sessions.Choose(directory, nil, nil)
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
		Run: func(_ slash.Context, arguments []string) error {
			if !slices.Equal(arguments, []string{"one", "two"}) {
				t.Errorf("got arguments %v", arguments)
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

func TestSlashCommandCanAddANotice(t *testing.T) {
	fixtureCommands := fixtureCommandRegistry(t, slash.Command{
		Name: "fixture",
		Run: func(context slash.Context, _ []string) error {
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
	if len(self.events) != 1 {
		t.Fatalf("got events %v", self.events)
	}
	if self.events[0].Kind != agent.HarnessMessageEvent || self.events[0].Text != "fixture notice" {
		t.Errorf("got event %v", self.events[0])
	}
}

func TestUnknownSlashCommandShowsAnErrorAndKeepsTheInput(t *testing.T) {
	self := slashCommandFixture(t, caps.Read)
	self.screen = output.New(&bytes.Buffer{})
	self.commands = fixtureCommandRegistry(t)
	editor := edit.NewInput(nil)
	for _, value := range "/unknown" {
		editor.Apply(key.Key{Code: key.Rune, Value: value}, false)
	}

	self.acceptInput(editor, nil)

	if got := editor.Text(); got != "/unknown" {
		t.Errorf("got input %q", got)
	}
	if len(self.events) != 1 {
		t.Fatalf("got events %v", self.events)
	}
	want := "Command not found: /unknown (alt+enter sends as message)"
	if self.events[0].Text != want || self.events[0].Status != agent.ErrorStatus {
		t.Errorf("got event %+v, want failed %q", self.events[0], want)
	}
}

func TestSnippetKeepsItsInvocationInHistoryAndQueuesItsRenderedPrompt(t *testing.T) {
	self := slashCommandFixture(t, caps.Read)
	self.commands = fixtureSnippetRegistry(t, map[string]string{
		"add": "Add the following:\n\n{{ .Arg }}",
	})
	self.currentTurn = Turn{Stream: testTurnStream(nil, func() {}, turn.State{Running: true})}

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

func TestSnippetWithoutArgumentsShowsUsageAndKeepsTheInput(t *testing.T) {
	self := slashCommandFixture(t, caps.Read)
	self.screen = output.New(&bytes.Buffer{})
	self.commands = fixtureSnippetRegistry(t, map[string]string{
		"add": "Add the following:\n\n{{ .Arg }}",
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
	if len(self.events) != 1 || self.events[0].Text != "Usage: //add <args> (alt+enter sends as message)" {
		t.Errorf("got events %+v", self.events)
	}
}

func TestPlainSnippetInputWaitsForTheRenderedPrompt(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	self.commands = fixtureSnippetRegistry(t, map[string]string{
		"ask": "Question: {{index .Args 0}} / {{.Arg}}",
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
	self.commands = fixtureSnippetRegistry(t, map[string]string{
		"review": "{{index .Args 2}}",
	})

	if got := self.handleCommand("//review only-one"); got != dispatch.Rejected {
		t.Fatalf("got slash input result %d", got)
	}
	if len(self.events) != 1 || !strings.HasPrefix(self.events[0].Text, "//review: Could not render template:") {
		t.Errorf("got events %+v", self.events)
	}
	if !strings.HasSuffix(self.events[0].Text, " (alt+enter sends as message)") {
		t.Errorf("expected the way out to be offered, got %q", self.events[0].Text)
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
	want := "Command not found: //unknown (alt+enter sends as message)"
	if len(self.events) != 1 || self.events[0].Text != want || self.events[0].Status != agent.ErrorStatus {
		t.Errorf("got events %+v, want failed %q", self.events, want)
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
	snippetSet, err := snippets.New(map[string]string{
		"test":   "Run tests.",
		"review": "Review changes.",
	})
	if err != nil {
		t.Fatal(err)
	}
	self := slashCommandFixture(t, caps.Read)
	self.commands = fixtureRegistry(t, systemSet, snippetSet)

	for input, want := range map[string]string{
		"/":  "/conf",
		"//": "//review",
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

func slashTestHandler(slash.Context, []string) error {
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

func fixtureSnippetRegistry(t *testing.T, configured map[string]string) slash.Registry {
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
	transition := cycle.Transition{Kind: cycle.NewSession, Arguments: []string{"-d", "/workspace"}}
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

func TestSlashCommandCanAddASuccessMessage(t *testing.T) {
	fixtureCommands := fixtureCommandRegistry(t, slash.Command{
		Name: "fixture",
		Run: func(context slash.Context, _ []string) error {
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
	if len(self.events) != 1 || self.events[0].Status != agent.SuccessStatus {
		t.Errorf("got events %+v", self.events)
	}
}

func TestUsageErrorIsNotPrefixedWithTheCommandName(t *testing.T) {
	self := slashCommandFixture(t, caps.Read)
	self.screen = output.New(&bytes.Buffer{})
	self.commands = fixtureCommandRegistry(t, slash.Command{
		Name: "copy",
		Run: func(slash.Context, []string) error {
			return slash.Usage()
		},
	}.WithArguments("session-name", "session-id", "session-dir"))

	if got := self.handleCommand("/copy"); got != dispatch.Rejected {
		t.Fatalf("got slash input result %d", got)
	}
	want := "Usage: /copy {session-dir|session-id|session-name} (alt+enter sends as message)"
	if len(self.events) != 1 || self.events[0].Text != want {
		t.Errorf("got events %+v, want %q", self.events, want)
	}
}

func TestARefusedCommandKeepsWhatWasTypedAndSaysWhy(t *testing.T) {
	self := slashCommandFixture(t, caps.Read)
	self.screen = output.New(&bytes.Buffer{})
	self.commands = fixtureCommandRegistry(t, slash.Command{
		Name: "new",
		Run: func(slash.Context, []string) error {
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
	if len(self.events) != 1 || self.events[0].Text != want {
		t.Errorf("got events %+v, want %q", self.events, want)
	}
}

const sessionGoldenSystemPrompt = "You are a test assistant."

type sessionGoldenResponse struct {
	Events               []string          `toml:"event"`
	Lines                []string          `toml:"line"`
	Body                 string            `toml:"body"`
	Headers              map[string]string `toml:"headers"`
	Status               int               `toml:"status"`
	CancelAfterWireEvent int               `toml:"cancel-after-wire-event"`
	ResetAfterWireEvent  int               `toml:"reset-after-wire-event"`
	WaitForCancellation  bool              `toml:"wait-for-cancellation"`
}

type sessionGoldenTurn struct {
	Prompt                    string                  `toml:"prompt"`
	Responses                 []sessionGoldenResponse `toml:"response"`
	Timeout                   string                  `toml:"timeout"`
	IsCancelled               bool                    `toml:"is-cancelled"`
	CancelAfterReasoningDelta int                     `toml:"cancel-after-reasoning-delta"`
	CancelAfterReasoningEvent int                     `toml:"cancel-after-reasoning-event"`
	CancelAfterMessageDelta   int                     `toml:"cancel-after-message-delta"`
}

type sessionGoldenTool struct {
	Name          string   `toml:"name"`
	Outputs       []string `toml:"outputs"`
	StateKey      string   `toml:"state-key"`
	ShellWithheld bool     `toml:"shell-withheld"`
	WebWithheld   bool     `toml:"web-withheld"`
	WebAnswer     string   `toml:"web-answer"`
}

type sessionGoldenScenario struct {
	Name            string              `toml:"-"`
	Provider        string              `toml:"provider"`
	Model           string              `toml:"model"`
	Effort          string              `toml:"effort"`
	FirstTokenError string              `toml:"first-token-error"`
	Tools           []sessionGoldenTool `toml:"tool"`
	FirstTurn       sessionGoldenTurn   `toml:"first"`
	ResumeTurn      sessionGoldenTurn   `toml:"resume"`
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
		client, err := chat.New(endpoint, "test-token", scenario.Model, scenario.Effort, 128_000)
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

		if specification.WebWithheld || specification.WebAnswer != "" {
			isGranted := specification.WebAnswer != ""
			searcher := sessionGoldenSearcher{answer: specification.WebAnswer}
			tools = append(tools, web.New(func() bool { return isGranted }, searcher)...)
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
	processes := sandbox.NewProcesses(false)
	t.Cleanup(func() { _, _ = processes.Disable() })

	return shell.New(workspace, t.TempDir(), t.TempDir(), shell.Paths{}, mode, files, processes)
}

func serveSessionGoldenResponse(
	writer http.ResponseWriter,
	request *http.Request,
	response sessionGoldenResponse,
	cancelSignals chan<- struct{},
) {
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

func runSessionGoldenScenario(t *testing.T, scenario sessionGoldenScenario) map[string]string {
	t.Helper()

	responses := append(slices.Clone(scenario.FirstTurn.Responses), scenario.ResumeTurn.Responses...)
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
			http.Error(writer, "scenario has no response for this request", http.StatusInternalServerError)
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
		newSessionGoldenProvider(t, scenario, server.URL, scenario.FirstTokenError),
		newSessionGoldenTools(t, scenario.Tools),
	)
	var firstScreenOutput bytes.Buffer
	firstHarness := &App{
		agent:    firstAssistant,
		screen:   output.NewTerminalOfSize(&firstScreenOutput, replayColumns, replayLines),
		recorder: record.New(log),
	}
	firstHarness.currentTurn = Turn{Stream: testRunningTurnStream(), painter: firstHarness.newPainter(true)}
	runSessionGoldenTurn(t, firstHarness, scenario.FirstTurn, cancelSignals)

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
		newSessionGoldenProvider(t, scenario, server.URL, ""),
		newSessionGoldenTools(t, scenario.Tools),
	)
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
	resumedHarness.currentTurn = Turn{Stream: testRunningTurnStream()}
	resumedHarness.replay()
	requireSameVisibleScreen(
		t,
		"first interactive turn differs after resume",
		firstScreenOutput.String(),
		screenOutput.String(),
	)
	runSessionGoldenTurn(t, resumedHarness, scenario.ResumeTurn, cancelSignals)

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

	ansi := strings.TrimRight(visibleEscapes(screenOutput.String()), "\n") + "\n"
	settledScreen := strings.Join(liveScreen, "\n") + "\n"

	requestMutex.Lock()
	capturedRequestBodies := slices.Clone(requestBodies)
	requestMutex.Unlock()
	requests := canonicalProviderRequests(t, capturedRequestBodies)

	return map[string]string{
		".jsonl":          canonicalSessionJournal(t, directory, sessionName),
		".meta.json":      canonicalSessionMeta(t, directory, sessionName),
		".ansi":           ansi,
		".screen":         settledScreen,
		".transcript":     canonicalSessionTranscript(string(transcript), sessionName),
		".requests.jsonl": requests,
	}
}

func canonicalProviderRequests(t *testing.T, requestBodies [][]byte) string {
	t.Helper()

	var canonical bytes.Buffer
	for _, requestBody := range requestBodies {
		var request map[string]any
		if err := json.Unmarshal(requestBody, &request); err != nil {
			t.Fatal(err)
		}

		delete(request, "prompt_cache_key")
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

func runSessionGoldenTurn(
	t *testing.T,
	testHarness *App,
	turn sessionGoldenTurn,
	cancelSignals <-chan struct{},
) {
	t.Helper()

	var streamContext context.Context
	var cancel context.CancelFunc
	if turn.Timeout == "" {
		streamContext, cancel = context.WithCancel(t.Context())
	} else {
		timeout, err := time.ParseDuration(turn.Timeout)
		if err != nil {
			t.Fatal(err)
		}
		streamContext, cancel = context.WithTimeout(t.Context(), timeout)
	}
	defer cancel()
	testHarness.currentTurn.Stream = testRunningTurnStreamWithCancel(cancel)
	editor := edit.NewInput(nil)
	interruptWithEscape := func() {
		if !testHarness.apply(editor, nil, key.Key{Code: key.Escape}) {
			t.Fatal("escape closed the harness")
		}
	}

	go func() {
		select {
		case <-cancelSignals:
			cancel()
		case <-streamContext.Done():
		}
	}()

	reasoningDeltas := 0
	reasoningEvents := 0
	messageDeltas := 0
	for update, streamError := range testHarness.agent.Stream(streamContext, turn.Prompt) {
		testHarness.takeTurn(TurnEvent{Update: update, Err: streamError})
		if update.Delta != nil {
			switch update.Delta.Kind {
			case agent.ModelReasoningEvent:
				reasoningDeltas++
				if reasoningDeltas == turn.CancelAfterReasoningDelta {
					interruptWithEscape()
				}
			case agent.ModelMessageEvent:
				messageDeltas++
				if messageDeltas == turn.CancelAfterMessageDelta {
					interruptWithEscape()
				}
			}
		}
		if update.Event != nil && update.Event.Kind == agent.ModelReasoningEvent {
			reasoningEvents++
			if reasoningEvents == turn.CancelAfterReasoningEvent {
				interruptWithEscape()
			}
		}
	}
	testHarness.currentTurn.SetCancelled(turn.IsCancelled)
	testHarness.finish()
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
	transcriptStartedPattern  = regexp.MustCompile(`(?m)^- \*\*Started:\*\* ` + "`[^`]+`$")
	transcriptEventPattern    = regexp.MustCompile(`(?m)^> [^\n]+$`)
	transcriptDurationPattern = regexp.MustCompile(`(?m)^- \*\*Duration:\*\* ` + "`[^`]+`$")
)

func canonicalSessionTranscript(transcript string, sessionName string) string {
	canonical := strings.ReplaceAll(transcript, sessionName, "brave-otter")
	canonical = transcriptStartedPattern.ReplaceAllString(
		canonical,
		"- **Started:** `"+transcriptTime.Format(time.RFC3339Nano)+"`",
	)
	canonical = transcriptDurationPattern.ReplaceAllString(canonical, "- **Duration:** `0s`")

	eventIndex := 0
	return transcriptEventPattern.ReplaceAllStringFunc(canonical, func(string) string {
		when := transcriptTime.Add(time.Duration(eventIndex) * time.Second)
		eventIndex++
		return "> " + when.Format(time.RFC3339Nano)
	})
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

func testRunningTurnStreamWithCancel(cancel context.CancelFunc) *turn.Stream {
	return testTurnStream(nil, cancel, turn.State{Running: true})
}

func testTurnStream(events chan TurnEvent, cancel context.CancelFunc, state turn.State) *turn.Stream {
	if events == nil {
		events = make(chan TurnEvent)
	}
	if cancel == nil {
		cancel = func() {}
	}
	if state.StartedAt.IsZero() {
		state.StartedAt = time.Now()
	}
	return turn.Adopt(events, cancel, state)
}

func TestTurnElapsedKeepsTheCompletedTurnDuration(t *testing.T) {
	startedAt := time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC)
	harness := App{currentTurn: Turn{
		Stream: testTimedTurnStream(false, startedAt, startedAt.Add(69*time.Second)),
	}}

	isRunning, took, known := harness.turnElapsed()
	if isRunning || !known || took != 69*time.Second {
		t.Errorf("got running=%v, took=%s, known=%v", isRunning, took, known)
	}
}

func TestTurnElapsedIsUnknownBeforeTheFirstTurn(t *testing.T) {
	isRunning, took, known := (&App{}).turnElapsed()
	if isRunning || known || took != 0 {
		t.Errorf("got running=%v, took=%s, known=%v", isRunning, took, known)
	}
}

func TestVersionIsNotEmpty(t *testing.T) {
	if version() == "" {
		t.Error("expected a version, got nothing")
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

	held.segmentLayout = built

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
		Run: func(context slash.Context, _ []string) error {
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
