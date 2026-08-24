package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/dynamic"
	"crdx.org/io/cmd/oh/edit"
	"crdx.org/io/cmd/oh/input"
	"crdx.org/io/cmd/oh/key"
	"crdx.org/io/cmd/oh/metrics"
	"crdx.org/io/cmd/oh/output"
	"crdx.org/io/cmd/oh/recording"
	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/store"
	"crdx.org/io/cmd/oh/terminal"
	"crdx.org/io/cmd/oh/tty"
	"crdx.org/io/cmd/oh/turn"
	"crdx.org/io/internal/sandbox"
)

type SessionLogger = recording.Session

func recordSession(session SessionLogger) *recording.Recorder {
	return recording.New(session)
}

type Harness struct {
	agent         *agent.Agent
	events        []agent.Event
	screen        *output.Screen
	recorder      *recording.Recorder
	processes     *sandbox.Processes
	segmentLayout segment.Layout
	editor        *edit.Input
	mode          *caps.Mode
	terminal      terminal.Terminal
	metrics       metrics.Tracker

	workspaceDir       string
	enabledToolNames   []string
	restart            []string
	getOnWithItMessage string
	terminalFocused    bool

	queuedTurn     turn.Queue
	currentTurn    Turn
	onTurnFinished func()
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

	defer func() { self.screen.Release(self.recorder.Stored()) }()

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
		self.queuedTurn.MarkModeChange()
		self.interruptTurn()
	}
}

func (self *Harness) cancelTurn() {
	if self.currentTurn.isCancelled {
		self.queuedTurn.Clear()
	}

	self.interruptTurn()
}

func (self *Harness) replaceTurn(message string) {
	self.queuedTurn.Replace(message)
	self.interruptTurn()
}

func (self *Harness) restartArguments() []string {
	var arguments []string

	if self.recorder.Stored() {
		arguments = append(arguments, "-r", self.recorder.Name())
	} else {
		arguments = append(arguments, "--workspace", self.workspaceDir)
	}

	arguments = append(arguments, "--caps", self.mode.Current().Flags())
	for _, name := range self.enabledToolNames {
		arguments = append(arguments, "--tool", name)
	}

	return arguments
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

func (self *Harness) isTurnRunning() bool {
	return self.currentTurn.isRunning
}

func (self *Harness) turnElapsed() (bool, time.Duration, bool) {
	if self.currentTurn.startedAt.IsZero() {
		return false, 0, false
	}
	if self.currentTurn.isRunning {
		return true, time.Since(self.currentTurn.startedAt), true
	}

	return false, self.currentTurn.finishedAt.Sub(self.currentTurn.startedAt), true
}

func (self *Harness) turnCount() int {
	return self.metrics.TurnCount()
}

func (self *Harness) lastTurnTokenRate() (float64, bool) {
	return self.metrics.LastTurnTokenRate()
}

func (self *Harness) contextUsage() (int, int) {
	return self.metrics.ContextUsage()
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

	self.recorder.Resume(len(storedSession.Items))
	self.events = append(self.events, storedSession.Events...)

	self.metrics.Restore(storedSession.Events)

	self.screen.Reset()
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
	var previousPainter *Painter
	if self.currentTurn.isRunning {
		previousPainter = self.currentTurn.painter
		provisional = previousPainter.provisionalDelta()
		previousPainter.stop()
	}

	self.screen.Synchronise(func() {
		self.screen.Reset()
		self.replay()
		if provisional.Text != "" {
			self.currentTurn.painter.drawRestoredDelta(provisional, previousPainter.answerRenderer)
		}
	})
}

func (self *Harness) start(message string) {
	self.metrics.BeginTurn()

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
		self.metrics.RecordDelta(*turnEvent.update.Delta)
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
	self.metrics.Record(event)

	self.events = append(self.events, event)
	self.currentTurn.painter.drawEvent(event)

	if self.currentTurn.painter.isStale {
		self.redraw()
	}

	if err := self.recorder.Event(event); err != nil {
		self.notifyFailure("the conversation could not be stored: " + err.Error())
	}
	self.showStorageWarnings()
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

	_ = self.recorder.Event(event)
}

func (self *Harness) noticePainter() *Painter {
	if self.currentTurn.isRunning {
		return self.currentTurn.painter
	}

	return self.newPainter(false)
}

func (self *Harness) finish() {
	self.currentTurn.finishedAt = time.Now()
	self.screen.ReportProgress(false)

	if self.currentTurn.isCancelled {
		self.recordEvent(agent.Event{Kind: agent.InterruptionEvent})
	} else if self.currentTurn.err != nil {
		self.recordEvent(agent.Event{Kind: agent.FailureEvent, Text: self.currentTurn.err.Error()})
	}

	self.storeProviderState()
	self.showStorageWarnings()
	self.currentTurn.painter.close(dynamic.Cancelled)
	if self.currentTurn.painter.isStale {
		self.redraw()
	}
	self.screen.End()

	self.metrics.FinishTurn()

	self.currentTurn.isRunning = false
	self.currentTurn.events = nil

	if !self.currentTurn.isCancelled && self.queuedTurn.Empty() && !self.terminalFocused && self.onTurnFinished != nil {
		self.onTurnFinished()
	}

	queued, message := self.queuedTurn.Take()
	switch queued {
	case turn.Replacement:
		self.start(message)
	case turn.ModeChange:
		if message := self.mode.Inject(); message != "" {
			self.start(message)
		}
	case turn.None:
	}
}

func (self *Harness) storeProviderState() bool {
	items, err := self.agent.Dump()
	if err != nil {
		self.notifyFailure("the conversation state could not be stored: " + err.Error())
		return false
	}

	if err := self.recorder.StoreItems(items); err != nil {
		self.notifyFailure(err.Error())
		return false
	}

	return true
}

func (self *Harness) showStorageWarnings() {
	for _, err := range self.recorder.TakeWarnings() {
		self.notifyFailure(err.Error())
	}
}
