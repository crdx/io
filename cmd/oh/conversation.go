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
	"crdx.org/io/cmd/oh/theme"
	"crdx.org/io/cmd/oh/tty"
	"crdx.org/io/internal/sandbox"
)

type conversation struct {
	assistant    *agent.Agent                 // the conversation driver
	screen       *output.Output               // where the conversation is drawn
	log          *store.Writer                // where the conversation is stored
	label        func(bool, int, bool) string // what the harness was started with, drawn afresh on the rule
	workspaceDir string                       // where the conversation is being held
	mode         *Mode                        // what the tools allow
	processes    *sandbox.Processes           // what background commands belong to this conversation
	shell        string                       // what the shell tool was named, taken from the tool itself

	restart            []string // the arguments to start again with, once the terminal has been given back
	queuedPrompt       string   // what to ask as soon as an interrupted turn finishes
	queuedTurn         bool     // whether an interrupted turn has a replacement
	queuedModeChange   bool     // whether changed capabilities should restart an interrupted turn
	getOnWithItMessage string   // what an empty double enter sends

	turn        turn    // the turn in progress
	storedItems int     // how many provider items have been stored
	transcript  []entry // the conversation as it was drawn, so it can be drawn again
}

type entry struct {
	event  agent.Event // what the agent said or did
	notice string      // what the harness said itself, painted as it was said
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
			if !self.turn.running {
				continue
			}
			self.turn.frame++
		}

		self.show(input)
	}
}

func (self *conversation) apply(input *line.Input, history *line.History, keypress key.Key) bool {
	switch input.Apply(keypress, self.turn.running) {
	case line.Accept:
		self.acceptInput(input, history)

	case line.Continue:
		self.submitInput(input, history, self.getOnWithItMessage)

	case line.Cancel:
		self.cancelTurn()

	case line.Quit:
		return false // the input only asks to leave where there was no turn to stop first

	case line.Restart:
		if self.turn.running {
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
					self.notify(theme.Stopped("Background processes killed (" + strings.Join(names, ", ") + ")"))
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
	input.Reset()

	if message == "" {
		return
	}

	history.Add(message)

	if self.turn.running {
		self.replaceTurn(message)
	} else {
		self.start(message)
	}
}

func (self *conversation) toggleCapability(whichCaps caps) {
	self.mode.Toggle(whichCaps)

	if self.turn.running {
		self.queuedModeChange = true
		self.interrupt()
	}
}

func (self *conversation) cancelTurn() {
	if self.turn.cancelled {
		self.queuedPrompt = ""
		self.queuedTurn = false
		self.queuedModeChange = false
	}

	self.interrupt()
}

func (self *conversation) replaceTurn(prompt string) {
	self.queuedPrompt = prompt
	self.queuedTurn = true
	self.interrupt()
}

func (self *conversation) restartArguments() []string {
	var arguments []string

	if self.log.Stored() {
		arguments = append(arguments, "--resume", self.log.ID())
	} else {
		arguments = append(arguments, "--workspace", self.workspaceDir)
	}

	return append(arguments, "--caps", self.mode.Current().Flags())
}

func (self *conversation) interrupt() {
	if !self.turn.running {
		return
	}

	self.turn.cancelled = true
	self.turn.stop()
}

func (self *conversation) show(input *line.Input) {
	width := self.screen.Columns()
	frame := input.Frame(width)

	framedRows := append([]string{rule(width, scrollLabel("↑", frame.Above), "")}, frame.Rows...)
	framedRows = append(framedRows, bannerRule(
		width,
		self.label(input.Pending(), self.turn.frame, self.turn.running),
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

const settling = 100 * time.Millisecond // a drag sends a signal for every column it passes through

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
	if err := self.assistant.Load(storedSession.Items); err != nil {
		self.notify(theme.Failure("the conversation could not be restored: " + err.Error()))
		return
	}
	self.storedItems = len(storedSession.Items)

	for _, event := range storedSession.Events {
		self.transcript = append(self.transcript, entry{event: event})
	}

	self.replay()
}

func (self *conversation) newPicasso(live bool) *painter {
	return &painter{
		screen: self.screen, live: live, tools: self.assistant.Tool, shell: self.shell,
		workspaceDir: self.workspaceDir,
	}
}

func (self *conversation) replay() {
	self.screen.Synchronise(func() {
		painter := self.newPicasso(self.turn.running)

		for _, record := range self.transcript {
			if record.notice != "" {
				painter.close(status.Cancelled)
				self.screen.Line(record.notice)

				continue
			}

			painter.draw(record.event)
		}

		if self.turn.running {
			self.turn.painter = painter // unanswered calls are on an open block again, on the same rows
			return
		}

		painter.close(status.Cancelled)

		self.screen.End()
	})
}

func (self *conversation) redraw() {
	if self.turn.running {
		self.turn.painter.stop() // its ticker would draw over the replay
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
		running: true,
		stop:    stop,
		events:  make(chan turnEvent),
		painter: self.newPicasso(true),
	}

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

	if self.turn.painter.stale {
		self.redraw()
	}

	self.writeSessionEvents(self.turn.pendingEvents.Add(event))
	self.showStorageWarnings()
}

func appendTranscript(transcript []entry, event agent.Event) []entry {
	if event.Kind == agent.Text && len(transcript) > 0 {
		if last := &transcript[len(transcript)-1]; last.event.Kind == agent.Text {
			last.event.Text += event.Text // a long answer is one entry, not thousands of deltas
			return transcript
		}
	}

	return append(transcript, entry{event: event})
}

func (self *conversation) notify(notice string) {
	self.transcript = append(self.transcript, entry{notice: notice})
	self.screen.Line(notice)
}

func (self *conversation) finish() {
	if self.turn.cancelled {
		self.recordEvent(agent.Event{Kind: agent.Interrupted})
	}

	self.flush(&self.turn.pendingEvents)
	self.storeItems()
	self.showStorageWarnings()
	self.turn.painter.close(status.Cancelled)
	self.screen.End()

	if !self.turn.cancelled && self.turn.failure != nil {
		self.notify(theme.Failure(self.turn.failure.Error()))
	}

	self.screen.End()

	self.turn.running = false
	self.turn.events = nil

	if self.queuedTurn {
		prompt := self.queuedPrompt
		self.queuedPrompt = ""
		self.queuedTurn = false
		self.queuedModeChange = false
		self.start(prompt)
	} else if self.queuedModeChange {
		self.queuedModeChange = false
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
		self.notify(theme.Failure("the conversation state could not be stored: " + err.Error()))
		return
	}

	if len(items) < self.storedItems {
		self.notify(theme.Failure("the provider replaced append-only conversation state"))
		return
	}

	for _, item := range items[self.storedItems:] {
		if err := self.log.Item(item); err != nil {
			self.notify(theme.Failure("the conversation state could not be stored: " + err.Error()))
			return
		}

		self.storedItems++
	}
}

func (self *conversation) write(record func() error) {
	if err := record(); err != nil {
		self.notify(theme.Failure("the conversation could not be stored: " + err.Error()))
	}
}

func (self *conversation) showStorageWarnings() {
	for _, err := range self.log.TakeWarnings() {
		self.notify(theme.Failure(err.Error()))
	}
}
