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
	"crdx.org/io/cmd/oh/key"
	"crdx.org/io/cmd/oh/output"
	"crdx.org/io/cmd/oh/spinner"
	"crdx.org/io/cmd/oh/status"
	"crdx.org/io/cmd/oh/store"
	"crdx.org/io/cmd/oh/tty"
	"crdx.org/io/internal/sandbox"
)

type Harness struct {
	agent        *agent.Agent
	screen       *output.Screen
	log          *store.Writer
	processes    *sandbox.Processes // what background commands belong to this conversation
	workspaceDir string
	mode         *caps.Mode
	shell        string

	restart            []string   // the arguments to start again with, once the terminal has been given back
	queuedTurn         QueuedTurn // what an interrupted turn is to be followed by
	getOnWithItMessage string     // what an empty double enter sends

	turn            Turn
	onTurnFinished  func()
	flushBoundary   int  // how many provider items have been stored
	terminalFocused bool // whether the interactive terminal has focus

	events []agent.Event

	label func(bool, bool, int) string // what the harness was started with
}

const historyLimit = 1000

func (self *Harness) makeIntroductions(initialMessage string) {
	history := edit.NewHistory(historyPath(), historyLimit)
	input := edit.NewInput(history)

	restore, err := tty.Raw(os.Stdin, os.Stdout)
	if err != nil {
		self.plainly(history, initialMessage)
		return
	}

	self.terminalFocused = true

	defer restore()
	defer func() { self.screen.Release(self.log.Stored()) }()

	keys := keypresses(os.Stdin)
	resizeSignals := resizes()
	frames := time.NewTicker(spinner.Activity.Rate())
	defer frames.Stop()

	self.show(input)

	if initialMessage != "" {
		history.Add(initialMessage)
		self.start(initialMessage)
	}

	for {
		select {
		case keypress, open := <-keys:
			if !open {
				return
			}

			if !self.apply(input, history, keypress) {
				return
			}

		case report, running := <-self.turn.events:
			if running {
				self.takeTurn(report)
			} else {
				self.finish()
			}

		case <-resizeSignals:
			settle(resizeSignals)
			self.redraw()

		case <-frames.C:
			if !self.turn.isRunning {
				continue
			}
			self.turn.spinnerFrame++
		}

		self.show(input)
	}
}

func (self *Harness) apply(input *edit.Input, history *edit.History, keypress key.Key) bool {
	switch keypress.Code {
	case key.FocusIn:
		self.terminalFocused = true
		return true
	case key.FocusOut:
		self.terminalFocused = false
		return true
	}

	switch input.Apply(keypress, self.turn.isRunning) {
	case edit.Accept:
		self.acceptInput(input, history)

	case edit.Continue:
		self.submitInput(input, history, self.getOnWithItMessage)

	case edit.Cancel:
		self.cancelTurn()

	case edit.Quit:
		return false

	case edit.Restart:
		if self.turn.isRunning {
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

func (self *Harness) acceptInput(input *edit.Input, history *edit.History) {
	self.submitInput(input, history, strings.TrimSpace(input.Text()))
}

func (self *Harness) submitInput(input *edit.Input, history *edit.History, message string) {
	history.Add(message)
	input.Reset()

	if message == "" {
		return
	}

	if self.turn.isRunning {
		self.replaceTurn(message)
	} else {
		self.start(message)
	}
}

func (self *Harness) toggleCapability(whichCaps caps.Set) {
	self.mode.Toggle(whichCaps)

	if self.turn.isRunning {
		self.queuedTurn.isModeChange = true
		self.interruptTurn()
	}
}

func (self *Harness) cancelTurn() {
	if self.turn.isCancelled {
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
	if !self.turn.isRunning {
		return
	}

	self.turn.isCancelled = true
	self.turn.cancel()
}

func (self *Harness) show(input *edit.Input) {
	columns := self.screen.Columns()
	frame := input.Frame(columns)

	framedRows := append([]string{
		rule(columns, scrollLabel("↑", frame.Above), ""),
	}, frame.Rows...)

	inputLabel := self.label(
		input.IsPending(),
		self.turn.isRunning,
		self.turn.spinnerFrame,
	)

	framedRows = append(framedRows, bannerRule(
		columns,
		inputLabel,
		scrollLabel("↓", frame.Below),
	))

	self.screen.Footer(framedRows, frame.Row+1, frame.Column)
}

func scrollLabel(arrow string, rows int) string {
	if rows == 0 {
		return ""
	}

	return fmt.Sprintf("%s %d", arrow, rows)
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

	for event := range self.turn.events {
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
		painter := self.newPainter(self.turn.isRunning)

		for _, event := range self.events {
			painter.drawEvent(event)
		}

		if self.turn.isRunning {
			self.turn.painter = painter
			return
		}

		painter.close(status.Cancelled)

		self.screen.End()
	})
}

func (self *Harness) redraw() {
	if self.turn.isRunning {
		self.turn.painter.stop()
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

	self.turn = Turn{
		isRunning: true,
		cancel:    cancel,
		events:    make(chan TurnEvent),
		painter:   self.newPainter(true),
	}

	self.screen.ReportProgress(true)

	go func() {
		defer close(self.turn.events)
		defer cancel()

		for event, err := range self.agent.Stream(turnCtx, message) {
			self.turn.events <- TurnEvent{
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
		self.turn.err = turnEvent.err
		return
	}

	self.recordEvent(turnEvent.event)
}

func (self *Harness) recordEvent(event agent.Event) {
	self.events = appendTranscript(self.events, event)
	self.turn.painter.drawEvent(event)

	if self.turn.painter.isStale {
		self.redraw()
	}

	self.writeSessionEvents(self.turn.pendingEvents.Add(event))
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

	if self.turn.isRunning {
		self.flush(&self.turn.pendingEvents)
	}

	_ = self.log.Event(event)
}

func (self *Harness) noticePainter() *Painter {
	if self.turn.isRunning {
		return self.turn.painter
	}

	return self.newPainter(false)
}

func (self *Harness) finish() {
	self.screen.ReportProgress(false)

	if self.turn.isCancelled {
		self.recordEvent(agent.Event{Kind: agent.Interruption})
	} else if self.turn.err != nil {
		self.recordEvent(agent.Event{Kind: agent.Failure, Text: self.turn.err.Error()})
	}

	self.flush(&self.turn.pendingEvents)
	self.storeItems()
	self.showStorageWarnings()
	self.turn.painter.close(status.Cancelled)
	self.screen.End()

	self.turn.isRunning = false
	self.turn.events = nil

	if !self.turn.isCancelled && !self.queuedTurn.isReplacement && !self.queuedTurn.isModeChange &&
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
