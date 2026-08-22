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
	"crdx.org/io/cmd/oh/edit"
	"crdx.org/io/cmd/oh/input"
	"crdx.org/io/cmd/oh/key"
	"crdx.org/io/cmd/oh/output"
	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/status"
	"crdx.org/io/cmd/oh/store"
	"crdx.org/io/cmd/oh/tty"
	"crdx.org/io/internal/sandbox"
)

type Harness struct {
	agent         *agent.Agent
	events        []agent.Event
	screen        *output.Screen
	log           *store.Writer
	processes     *sandbox.Processes
	segmentLayout segment.Layout
	editor        *edit.Input
	mode          *caps.Mode

	workspaceDir       string
	shell              string
	restart            []string
	getOnWithItMessage string
	queuedTurn         QueuedTurn
	currentTurn        Turn
	onTurnFinished     func()
	flushBoundary      int
	terminalFocused    bool
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
	defer func() { self.screen.Release(self.log.Stored()) }()

	keys := keypresses(os.Stdin)
	resizeSignals := resizes()
	ticker := self.getTicker()
	defer ticker.Stop()

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
			if !self.currentTurn.isRunning {
				continue
			}
			self.currentTurn.spinnerFrame++
		}

		self.show(editor)
	}
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

	self.replay()
}

func (self *Harness) newPainter(isRunning bool) *Painter {
	return &Painter{
		screen:       self.screen,
		isRunning:    isRunning,
		getTool:      self.agent.Tool,
		shellName:    self.shell,
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

		painter.close(status.Cancelled)

		self.screen.End()
	})
}

func (self *Harness) redraw() {
	if self.currentTurn.isRunning {
		self.currentTurn.painter.stop()
	}

	self.screen.Synchronise(func() {
		self.screen.Reset()
		self.replay()
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
	}

	self.screen.ReportProgress(true)

	go func() {
		defer close(self.currentTurn.events)
		defer cancel()

		for event, err := range self.agent.Stream(turnCtx, message) {
			self.currentTurn.events <- TurnEvent{
				event: event,
				err:   err,
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

	self.recordEvent(turnEvent.event)
}

func (self *Harness) recordEvent(event agent.Event) {
	self.events = appendTranscript(self.events, event)
	self.currentTurn.painter.drawEvent(event)

	if self.currentTurn.painter.isStale {
		self.redraw()
	}

	self.writeSessionEvents(self.currentTurn.pendingEvents.Add(event))
	self.showStorageWarnings()
}

func appendTranscript(transcript []agent.Event, event agent.Event) []agent.Event {
	if (event.Kind == agent.ModelMessage || event.Kind == agent.ModelReasoning) && len(transcript) > 0 {
		if last := &transcript[len(transcript)-1]; last.Kind == event.Kind {
			last.Text += event.Text
			return transcript
		}
	}

	return append(transcript, event)
}

func (self *Harness) notifyFailure(text string) {
	self.notify(agent.Event{Kind: agent.HarnessMessage, Text: text, Failed: true})
}

func (self *Harness) notifyStopped(text string) {
	self.notify(agent.Event{Kind: agent.HarnessMessage, Text: text})
}

func (self *Harness) notify(event agent.Event) {
	self.events = append(self.events, event)
	self.noticePainter().drawEvent(event)

	if self.currentTurn.isRunning {
		self.flush(&self.currentTurn.pendingEvents)
	}

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
		self.recordEvent(agent.Event{Kind: agent.Interruption})
	} else if self.currentTurn.err != nil {
		self.recordEvent(agent.Event{Kind: agent.Failure, Text: self.currentTurn.err.Error()})
	}

	self.flush(&self.currentTurn.pendingEvents)
	self.storeItems()
	self.showStorageWarnings()
	self.currentTurn.painter.close(status.Cancelled)
	self.screen.End()

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

func (self *Harness) flush(pendingEvents *agent.Coalescer) {
	self.writeSessionEvents(pendingEvents.Flush())
}

func (self *Harness) writeSessionEvents(events []agent.Event) {
	for _, event := range events {
		self.write(func() error { return self.log.Event(event) })
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
