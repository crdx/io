package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/dynamic"
	"crdx.org/io/cmd/oh/edit"
	"crdx.org/io/cmd/oh/input"
	"crdx.org/io/cmd/oh/key"
	"crdx.org/io/cmd/oh/output"
	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/store"
	"crdx.org/io/cmd/oh/terminal"
	"crdx.org/io/cmd/oh/tty"
	"crdx.org/io/internal/sandbox"
	"crdx.org/io/internal/util"
)

type SessionLogger interface {
	Stored() bool
	Name() string
	Event(agent.Event) error
	Item(json.RawMessage) error
	TakeWarnings() []error
}

type Harness struct {
	agent         *agent.Agent
	events        []agent.Event
	screen        *output.Screen
	log           SessionLogger
	processes     *sandbox.Processes
	segmentLayout segment.Layout
	editor        *edit.Input
	mode          *caps.Mode
	terminal      terminal.Terminal

	workspaceDir        string
	contextWindowTokens int
	turnsTaken          int
	lastTurnRate        float64
	restart             []string
	getOnWithItMessage  string
	queuedTurn          QueuedTurn
	currentTurn         Turn
	onTurnFinished      func()
	flushBoundary       int
	terminalFocused     bool
}

const historyLimit = 1000

func (self *Harness) begin(message string) {
	history := edit.NewHistory(historyPath(), historyLimit)
	editor := edit.NewInput(history)
	self.editor = editor

	restoreTTY, err := tty.Raw(os.Stdin, os.Stdout)
	if err != nil {
		self.plainly(history, message)
		return
	}

	self.terminalFocused = true

	defer restoreTTY()

	restoreTerminal := self.terminal.Begin(self.mode.Current())
	defer restoreTerminal()

	defer func() { self.screen.Release(self.log.Stored()) }()

	keys := keypresses(os.Stdin)
	resizeSignals := resizes()
	ticker := self.getTicker()
	defer ticker.Stop()

	idle := idleRefresh{interval: self.segmentLayout.IdleRefreshInterval()}

	self.show(editor)

	if message != "" {
		history.Add(message)
		self.start(message)
	}

	for {
		select {
		case keypress, open := <-keys:
			if !open {
				return
			}

			if !self.apply(editor, history, keypress) {
				return
			}

		case report, running := <-self.currentTurn.events:
			if running {
				self.takeTurn(report)
			} else {
				self.finish()
			}

		case <-resizeSignals:
			settle(resizeSignals)
			self.redraw()

		case <-ticker.C:
			if self.currentTurn.isRunning {
				self.currentTurn.spinnerFrame++
			} else if !idle.isDue() {
				continue
			}
		}

		self.show(editor)
	}
}

type idleRefresh struct {
	interval time.Duration
	drawnAt  time.Time
}

func (self *idleRefresh) isDue() bool {
	if self.interval <= 0 || time.Since(self.drawnAt) < self.interval {
		return false
	}

	self.drawnAt = time.Now()

	return true
}

func (self *Harness) getTicker() *time.Ticker {
	interval := self.segmentLayout.RefreshInterval()
	if interval <= 0 {
		ticker := time.NewTicker(time.Hour)
		ticker.Stop()

		return ticker
	}

	return time.NewTicker(interval)
}

func (self *Harness) apply(editor *edit.Input, history *edit.History, keypress key.Key) bool {
	switch keypress.Code {
	case key.FocusIn:
		self.terminalFocused = true
		return true
	case key.FocusOut:
		self.terminalFocused = false
		return true
	}

	switch editor.Apply(keypress, self.currentTurn.isRunning) {
	case edit.Accept:
		self.acceptInput(editor, history)

	case edit.Continue:
		self.submitInput(editor, history, self.getOnWithItMessage)

	case edit.Cancel:
		self.cancelTurn()

	case edit.Quit:
		return false

	case edit.Restart:
		if self.currentTurn.isRunning {
			break
		}

		self.screen.Reset()
		self.restart = self.restartArguments()

		return false

	case edit.Write:
		self.toggleCapability(caps.Write)

	case edit.Shell:
		self.toggleCapability(caps.Shell)

	case edit.Git:
		self.toggleCapability(caps.Git)

	case edit.Background:
		if self.mode.Current().Has(caps.Background) {
			names, err := self.processes.Disable()
			if err == nil {
				self.toggleCapability(caps.Background)
				if len(names) > 0 {
					self.notifyStopped("Background processes killed (" + strings.Join(names, ", ") + ")")
				}
			} else {
				self.processes.Enable()
			}
		} else {
			self.processes.Enable()
			self.toggleCapability(caps.Background)
		}

	case edit.Drawn:
	}

	return true
}

func (self *Harness) acceptInput(editor *edit.Input, history *edit.History) {
	self.submitInput(editor, history, strings.TrimSpace(editor.Text()))
}

func (self *Harness) submitInput(editor *edit.Input, history *edit.History, message string) {
	history.Add(message)
	editor.Reset()

	if message == "" {
		return
	}

	if self.currentTurn.isRunning {
		self.replaceTurn(message)
	} else {
		self.start(message)
	}
}

func (self *Harness) toggleCapability(whichCaps caps.Set) {
	self.mode.Toggle(whichCaps)
	self.terminal.SetMode(self.mode.Current())

	if self.currentTurn.isRunning {
		self.queuedTurn.isModeChange = true
		self.interruptTurn()
	}
}

func (self *Harness) cancelTurn() {
	if self.currentTurn.isCancelled {
		self.queuedTurn = QueuedTurn{}
	}

	self.interruptTurn()
}

func (self *Harness) replaceTurn(message string) {
	self.queuedTurn.nextMessage = message
	self.queuedTurn.isReplacement = true
	self.interruptTurn()
}

func (self *Harness) restartArguments() []string {
	var arguments []string

	if self.log.Stored() {
		arguments = append(arguments, "-r", self.log.Name())
	} else {
		arguments = append(arguments, "--workspace", self.workspaceDir)
	}

	return append(arguments, "--caps", self.mode.Current().Flags())
}

func (self *Harness) interruptTurn() {
	if !self.currentTurn.isRunning {
		return
	}

	self.currentTurn.isCancelled = true
	self.currentTurn.cancel()
}

func (self *Harness) show(editor *edit.Input) {
	columns := self.screen.Columns()
	frame := editor.Frame(columns)

	block := input.Block{
		Top: input.Ruler{
			Left:   self.bar(segment.TopLeft, frame),
			Center: self.bar(segment.TopCenter, frame),
			Right:  self.bar(segment.TopRight, frame),
		},
		Input: frame,
		Bottom: input.Ruler{
			Left:   self.bar(segment.BottomLeft, frame),
			Center: self.bar(segment.BottomCenter, frame),
			Right:  self.bar(segment.BottomRight, frame),
		},
	}

	self.screen.Footer(block.Rows(columns))
}

func (self *Harness) bar(position segment.Position, frame edit.Frame) string {
	return bar(self.segmentLayout, position, frame)
}

func (self *Harness) turnActivity() (bool, int) {
	return self.currentTurn.isRunning, self.currentTurn.spinnerFrame
}

func (self *Harness) turnElapsed() (bool, time.Duration) {
	if !self.currentTurn.isRunning || self.currentTurn.startedAt.IsZero() {
		return false, 0
	}

	return true, time.Since(self.currentTurn.startedAt)
}

func (self *Harness) turnCount() int {
	return self.turnsTaken
}

func (self *Harness) lastTurnTokenRate() (float64, bool) {
	return self.lastTurnRate, self.lastTurnRate > 0
}

func (self *Harness) contextUsage() (int, int) {
	return contextUsageAt(self.events, self.contextWindowTokens)
}

func contextUsageAt(events []agent.Event, contextWindowTokens int) (int, int) {
	for _, event := range slices.Backward(events) {
		if event.Usage != nil && event.Usage.InputTokens > 0 {
			return event.Usage.InputTokens, contextWindowTokens
		}
	}

	return 0, contextWindowTokens
}

func (self *Harness) grantedCaps() caps.Set {
	return self.mode.Current()
}

func (self *Harness) isChordPending() bool {
	return self.editor != nil && self.editor.IsPending()
}

func (self *Harness) plainly(history *edit.History, initialMessage string) {
	if initialMessage != "" {
		self.ask(history, initialMessage)
	}

	reader := bufio.NewScanner(os.Stdin)

	for reader.Scan() {
		if stdin := strings.TrimSpace(reader.Text()); stdin != "" {
			self.ask(history, stdin)
		}
	}

	if err := reader.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "could not read input:", err)
	}
}

func (self *Harness) ask(history *edit.History, message string) {
	history.Add(message)
	self.start(message)

	for event := range self.currentTurn.events {
		self.takeTurn(event)
	}

	self.finish()
}

func keypresses(terminal *os.File) <-chan key.Key {
	keys := make(chan key.Key)

	go func() {
		defer close(keys)

		decoder := key.NewDecoder(bufio.NewReader(terminal))

		for {
			keypress, err := decoder.Next()
			if err != nil {
				return
			}

			keys <- keypress
		}
	}()

	return keys
}

func resizes() <-chan os.Signal {
	resizeSignals := make(chan os.Signal, 1)
	signal.Notify(resizeSignals, syscall.SIGWINCH)

	return resizeSignals
}

const settling = 100 * time.Millisecond

func settle(resizeSignals <-chan os.Signal) {
	time.Sleep(settling)

	for {
		select {
		case <-resizeSignals:
		default:
			return
		}
	}
}

func (self *Harness) restore(storedSession *store.Session) {
	if err := self.agent.RestoreState(storedSession.Events); err != nil {
		self.notifyFailure("the state could not be restored: " + err.Error())
		return
	}
	if err := self.agent.Load(storedSession.Items); err != nil {
		self.notifyFailure("the conversation could not be restored: " + err.Error())
		return
	}

	self.flushBoundary = len(storedSession.Items)
	self.events = append(self.events, storedSession.Events...)

	for _, event := range storedSession.Events {
		if event.Kind == agent.UserMessageEvent {
			self.turnsTaken++
		}
	}

	self.replay()
}

func (self *Harness) newPainter(isRunning bool) *Painter {
	return &Painter{
		screen:       self.screen,
		isRunning:    isRunning,
		getTool:      self.agent.Tool,
		workspaceDir: self.workspaceDir,
	}
}

func (self *Harness) replay() {
	self.screen.Synchronise(func() {
		painter := self.newPainter(self.currentTurn.isRunning)

		for _, event := range self.events {
			painter.drawEvent(event)
		}

		if self.currentTurn.isRunning {
			self.currentTurn.painter = painter
			return
		}

		painter.close(dynamic.Cancelled)

		self.screen.End()
	})
}

func (self *Harness) redraw() {
	var provisional agent.Delta
	if self.currentTurn.isRunning {
		provisional = self.currentTurn.painter.provisionalDelta()
		self.currentTurn.painter.stop()
	}

	self.screen.Synchronise(func() {
		self.screen.Reset()
		self.replay()
		if provisional.Text != "" {
			self.currentTurn.painter.drawDelta(provisional)
		}
	})
}

func (self *Harness) start(message string) {
	if fyi := self.mode.Inject(); fyi != "" {
		self.agent.FYI(fyi)
	}

	turnCtx, cancel := context.WithCancel(context.Background())

	self.currentTurn = Turn{
		isRunning: true,
		cancel:    cancel,
		events:    make(chan TurnEvent),
		painter:   self.newPainter(true),
		startedAt: time.Now(),
	}

	self.screen.ReportProgress(true)

	go func() {
		defer close(self.currentTurn.events)
		defer cancel()

		for update, err := range self.agent.Stream(turnCtx, message) {
			self.currentTurn.events <- TurnEvent{
				update: update,
				err:    err,
			}

			if err != nil {
				return
			}
		}
	}()
}

func (self *Harness) takeTurn(turnEvent TurnEvent) {
	if turnEvent.err != nil {
		self.currentTurn.err = turnEvent.err
		return
	}

	if turnEvent.update.Delta != nil {
		self.currentTurn.painter.drawDelta(*turnEvent.update.Delta)
		if self.currentTurn.painter.isStale {
			self.redraw()
		}
		return
	}

	if turnEvent.update.Event != nil {
		self.recordEvent(*turnEvent.update.Event)
	}
}

func (self *Harness) recordEvent(event agent.Event) {
	self.countTowardsTheBar(event)

	self.events = append(self.events, event)
	self.currentTurn.painter.drawEvent(event)

	if self.currentTurn.painter.isStale {
		self.redraw()
	}

	self.write(func() error { return self.log.Event(event) })
	self.showStorageWarnings()
}

func (self *Harness) countTowardsTheBar(event agent.Event) {
	switch event.Kind {
	case agent.UserMessageEvent:
		self.turnsTaken++
	case agent.ModelMessageEvent, agent.ModelReasoningEvent:
		self.currentTurn.streamedBytes += len(event.Text)
	}
}

func (self *Harness) recordTokenRate() {
	took := time.Since(self.currentTurn.startedAt).Seconds()
	if self.currentTurn.streamedBytes == 0 || took <= 0 {
		return
	}

	self.lastTurnRate = float64(util.EstimateTokenCount(self.currentTurn.streamedBytes)) / took
}

func (self *Harness) notifyFailure(text string) {
	self.notify(agent.Event{Kind: agent.HarnessMessageEvent, Text: text, Failed: true})
}

func (self *Harness) notifyStopped(text string) {
	self.notify(agent.Event{Kind: agent.HarnessMessageEvent, Text: text})
}

func (self *Harness) notify(event agent.Event) {
	self.events = append(self.events, event)
	self.noticePainter().drawEvent(event)

	_ = self.log.Event(event)
}

func (self *Harness) noticePainter() *Painter {
	if self.currentTurn.isRunning {
		return self.currentTurn.painter
	}

	return self.newPainter(false)
}

func (self *Harness) finish() {
	self.screen.ReportProgress(false)

	if self.currentTurn.isCancelled {
		self.recordEvent(agent.Event{Kind: agent.InterruptionEvent})
	} else if self.currentTurn.err != nil {
		self.recordEvent(agent.Event{Kind: agent.FailureEvent, Text: self.currentTurn.err.Error()})
	}

	self.storeItems()
	self.showStorageWarnings()
	self.currentTurn.painter.close(dynamic.Cancelled)
	if self.currentTurn.painter.isStale {
		self.redraw()
	}
	self.screen.End()

	self.recordTokenRate()

	self.currentTurn.isRunning = false
	self.currentTurn.events = nil

	if !self.currentTurn.isCancelled && !self.queuedTurn.isReplacement && !self.queuedTurn.isModeChange &&
		!self.terminalFocused && self.onTurnFinished != nil {
		self.onTurnFinished()
	}

	if self.queuedTurn.isReplacement {
		message := self.queuedTurn.nextMessage
		self.queuedTurn = QueuedTurn{}
		self.start(message)
	} else if self.queuedTurn.isModeChange {
		self.queuedTurn = QueuedTurn{}
		if message := self.mode.Inject(); message != "" {
			self.start(message)
		}
	}
}

func (self *Harness) storeItems() {
	items, err := self.agent.Dump()
	if err != nil {
		self.notifyFailure("the conversation state could not be stored: " + err.Error())
		return
	}

	if len(items) < self.flushBoundary {
		self.notifyFailure("the provider replaced append-only conversation state")
		return
	}

	for _, item := range items[self.flushBoundary:] {
		if err := self.log.Item(item); err != nil {
			self.notifyFailure("the conversation state could not be stored: " + err.Error())
			return
		}

		self.flushBoundary++
	}
}

func (self *Harness) write(record func() error) {
	if err := record(); err != nil {
		self.notifyFailure("the conversation could not be stored: " + err.Error())
	}
}

func (self *Harness) showStorageWarnings() {
	for _, err := range self.log.TakeWarnings() {
		self.notifyFailure(err.Error())
	}
}
