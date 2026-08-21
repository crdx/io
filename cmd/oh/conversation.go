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
	"crdx.org/io/cmd/oh/key"
	"crdx.org/io/cmd/oh/line"
	"crdx.org/io/cmd/oh/output"
	"crdx.org/io/cmd/oh/spinner"
	"crdx.org/io/cmd/oh/status"
	"crdx.org/io/cmd/oh/store"
	"crdx.org/io/cmd/oh/tty"
	"crdx.org/io/internal/sandbox"
)

type conversation struct {
	assistant          *agent.Agent
	screen             *output.Output
	log                *store.Writer
	label              func(bool, int, bool) string // what the harness was started with
	workspaceDir       string
	mode               *Mode              // what the tools allow
	processes          *sandbox.Processes // what background commands belong to this conversation
	shell              string             // what the shell tool was named, taken from the tool itself
	notifyTurnFinished func()             // how to say that the harness is waiting for input

	restart            []string   // the arguments to start again with, once the terminal has been given back
	queuedTurn         queuedTurn // what an interrupted turn is to be followed by
	getOnWithItMessage string     // what an empty double enter sends

	turn            turn
	storedItems     int           // how many provider items have been stored
	terminalFocused bool          // whether the interactive terminal has focus
	transcript      []agent.Event // the conversation as it was drawn, so it can be drawn again
}

const historyLimit = 1000

func (self *conversation) makeIntroductions(initialPrompt string) {
	history := line.NewHistory(historyPath(), historyLimit)
	input := line.NewInput(history)

	restore, err := tty.Raw(os.Stdin, os.Stdout)
	if err != nil {
		self.plainly(history, initialPrompt)
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

	if initialPrompt != "" {
		history.Add(initialPrompt)
		self.start(initialPrompt)
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
				self.take(report)
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
			self.turn.frame++
		}

		self.show(input)
	}
}

func (self *conversation) apply(input *line.Input, history *line.History, keypress key.Key) bool {
	switch keypress.Code {
	case key.FocusIn:
		self.terminalFocused = true
		return true
	case key.FocusOut:
		self.terminalFocused = false
		return true
	}

	switch input.Apply(keypress, self.turn.isRunning) {
	case line.Accept:
		self.acceptInput(input, history)

	case line.Continue:
		self.submitInput(input, history, self.getOnWithItMessage)

	case line.Cancel:
		self.cancelTurn()

	case line.Quit:
		return false

	case line.Restart:
		if self.turn.isRunning {
			break
		}

		self.screen.Reset()
		self.restart = self.restartArguments()

		return false

	case line.Write:
		self.toggleCapability(capWrite)

	case line.Shell:
		self.toggleCapability(capShell)

	case line.Git:
		self.toggleCapability(capGit)

	case line.Background:
		if self.mode.Current().has(capBackground) {
			names, err := self.processes.Disable()
			if err == nil {
				self.toggleCapability(capBackground)
				if len(names) > 0 {
					self.notifyStopped("Background processes killed (" + strings.Join(names, ", ") + ")")
				}
			} else {
				self.processes.Enable()
			}
		} else {
			self.processes.Enable()
			self.toggleCapability(capBackground)
		}

	case line.Drawn:
	}

	return true
}

func (self *conversation) acceptInput(input *line.Input, history *line.History) {
	self.submitInput(input, history, strings.TrimSpace(input.Text()))
}

func (self *conversation) submitInput(input *line.Input, history *line.History, message string) {
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

func (self *conversation) toggleCapability(whichCaps caps) {
	self.mode.Toggle(whichCaps)

	if self.turn.isRunning {
		self.queuedTurn.isModeChange = true
		self.interrupt()
	}
}

func (self *conversation) cancelTurn() {
	if self.turn.isCancelled {
		self.queuedTurn = queuedTurn{}
	}

	self.interrupt()
}

func (self *conversation) replaceTurn(prompt string) {
	self.queuedTurn.prompt = prompt
	self.queuedTurn.isReplacement = true
	self.interrupt()
}

func (self *conversation) restartArguments() []string {
	var arguments []string

	if self.log.Stored() {
		arguments = append(arguments, "-r", self.log.ID())
	} else {
		arguments = append(arguments, "--workspace", self.workspaceDir)
	}

	return append(arguments, "--caps", self.mode.Current().Flags())
}

func (self *conversation) interrupt() {
	if !self.turn.isRunning {
		return
	}

	self.turn.isCancelled = true
	self.turn.stop()
}

func (self *conversation) show(input *line.Input) {
	width := self.screen.Columns()
	frame := input.Frame(width)

	framedRows := append([]string{rule(width, scrollLabel("↑", frame.Above), "")}, frame.Rows...)
	inputLabel := self.label(input.IsPending(), self.turn.frame, self.turn.isRunning)

	framedRows = append(framedRows, bannerRule(
		width,
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

func (self *conversation) plainly(history *line.History, initialPrompt string) {
	if initialPrompt != "" {
		self.ask(history, initialPrompt)
	}

	reader := bufio.NewScanner(os.Stdin)

	for reader.Scan() {
		if typedInput := strings.TrimSpace(reader.Text()); typedInput != "" {
			self.ask(history, typedInput)
		}
	}

	if err := reader.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "could not read input:", err)
	}
}

func (self *conversation) ask(history *line.History, prompt string) {
	history.Add(prompt)
	self.start(prompt)

	for report := range self.turn.events {
		self.take(report)
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

func (self *conversation) restore(storedSession *store.Session) {
	if err := self.assistant.RestoreState(storedSession.Events); err != nil {
		self.notifyFailure("the state could not be restored: " + err.Error())
		return
	}
	if err := self.assistant.Load(storedSession.Items); err != nil {
		self.notifyFailure("the conversation could not be restored: " + err.Error())
		return
	}
	self.storedItems = len(storedSession.Items)

	self.transcript = append(self.transcript, storedSession.Events...)

	self.replay()
}

func (self *conversation) newPainter(isLive bool) *painter {
	return &painter{
		screen:       self.screen,
		isLive:       isLive,
		tools:        self.assistant.Tool,
		shell:        self.shell,
		workspaceDir: self.workspaceDir,
	}
}

func (self *conversation) replay() {
	self.screen.Synchronise(func() {
		painter := self.newPainter(self.turn.isRunning)

		for _, record := range self.transcript {
			painter.draw(record)
		}

		if self.turn.isRunning {
			self.turn.painter = painter
			return
		}

		painter.close(status.Cancelled)

		self.screen.End()
	})
}

func (self *conversation) redraw() {
	if self.turn.isRunning {
		self.turn.painter.stop()
	}

	self.screen.Synchronise(func() {
		self.screen.Reset()
		self.replay()
	})
}

func (self *conversation) start(prompt string) {
	if notice := self.mode.Inject(); notice != "" {
		self.assistant.Note(notice)
	}

	turnContext, stop := context.WithCancel(context.Background())

	self.turn = turn{
		isRunning: true,
		stop:      stop,
		events:    make(chan turnEvent),
		painter:   self.newPainter(true),
	}
	self.screen.Progress(true)

	events := self.turn.events

	go func() {
		defer close(events)
		defer stop()

		for event, err := range self.assistant.Stream(turnContext, prompt) {
			events <- turnEvent{event: event, err: err}

			if err != nil {
				return
			}
		}
	}()
}

func (self *conversation) take(report turnEvent) {
	if report.err != nil {
		self.turn.failure = report.err
		return
	}

	self.recordEvent(report.event)
}

func (self *conversation) recordEvent(event agent.Event) {
	self.transcript = appendTranscript(self.transcript, event)
	self.turn.painter.draw(event)

	if self.turn.painter.isStale {
		self.redraw()
	}

	self.writeSessionEvents(self.turn.pendingEvents.Add(event))
	self.showStorageWarnings()
}

func appendTranscript(transcript []agent.Event, event agent.Event) []agent.Event {
	if (event.Kind == agent.Text || event.Kind == agent.Reasoning) && len(transcript) > 0 {
		if last := &transcript[len(transcript)-1]; last.Kind == event.Kind {
			last.Text += event.Text
			return transcript
		}
	}

	return append(transcript, event)
}

func (self *conversation) notifyFailure(text string) {
	self.notify(agent.Event{Kind: agent.Notice, Text: text, Failed: true})
}

func (self *conversation) notifyStopped(text string) {
	self.notify(agent.Event{Kind: agent.Notice, Text: text})
}

func (self *conversation) notify(event agent.Event) {
	self.transcript = append(self.transcript, event)
	self.noticePainter().draw(event)

	if self.turn.isRunning {
		self.flush(&self.turn.pendingEvents)
	}

	_ = self.log.Event(event)
}

func (self *conversation) noticePainter() *painter {
	if self.turn.isRunning {
		return self.turn.painter
	}

	return self.newPainter(false)
}

func (self *conversation) finish() {
	self.screen.Progress(false)

	if self.turn.isCancelled {
		self.recordEvent(agent.Event{Kind: agent.Interrupted})
	} else if self.turn.failure != nil {
		self.recordEvent(agent.Event{Kind: agent.Failure, Text: self.turn.failure.Error()})
	}

	self.flush(&self.turn.pendingEvents)
	self.storeItems()
	self.showStorageWarnings()
	self.turn.painter.close(status.Cancelled)
	self.screen.End()

	self.turn.isRunning = false
	self.turn.events = nil

	if !self.turn.isCancelled && !self.queuedTurn.isReplacement && !self.queuedTurn.isModeChange &&
		!self.terminalFocused && self.notifyTurnFinished != nil {
		self.notifyTurnFinished()
	}

	if self.queuedTurn.isReplacement {
		prompt := self.queuedTurn.prompt
		self.queuedTurn = queuedTurn{}
		self.start(prompt)
	} else if self.queuedTurn.isModeChange {
		self.queuedTurn = queuedTurn{}
		if prompt := self.mode.Inject(); prompt != "" {
			self.start(prompt)
		}
	}
}

func (self *conversation) flush(pendingEvents *agent.Coalescer) {
	self.writeSessionEvents(pendingEvents.Flush())
}

func (self *conversation) writeSessionEvents(events []agent.Event) {
	for _, event := range events {
		self.write(func() error { return self.log.Event(event) })
	}
}

func (self *conversation) storeItems() {
	items, err := self.assistant.Dump()
	if err != nil {
		self.notifyFailure("the conversation state could not be stored: " + err.Error())
		return
	}

	if len(items) < self.storedItems {
		self.notifyFailure("the provider replaced append-only conversation state")
		return
	}

	for _, item := range items[self.storedItems:] {
		if err := self.log.Item(item); err != nil {
			self.notifyFailure("the conversation state could not be stored: " + err.Error())
			return
		}

		self.storedItems++
	}
}

func (self *conversation) write(record func() error) {
	if err := record(); err != nil {
		self.notifyFailure("the conversation could not be stored: " + err.Error())
	}
}

func (self *conversation) showStorageWarnings() {
	for _, err := range self.log.TakeWarnings() {
		self.notifyFailure(err.Error())
	}
}
