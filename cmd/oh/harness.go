package main

import (
	"bufio"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/dynamic"
	"crdx.org/io/cmd/oh/edit"
	"crdx.org/io/cmd/oh/input"
	"crdx.org/io/cmd/oh/interaction"
	"crdx.org/io/cmd/oh/key"
	"crdx.org/io/cmd/oh/metrics"
	"crdx.org/io/cmd/oh/output"
	"crdx.org/io/cmd/oh/painter"
	"crdx.org/io/cmd/oh/recording"
	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/slash"
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
	agent              *agent.Agent
	events             []agent.Event
	screen             *output.Screen
	recorder           *recording.Recorder
	processes          *sandbox.Processes
	segmentLayout      segment.Layout
	editor             *edit.Input
	mode               *caps.Mode
	settledCaps        caps.Set
	pendingModeChanges []int
	terminal           terminal.Terminal
	metrics            metrics.Tracker

	workspaceDir       string
	getOnWithItMessage string

	commands   slash.CommandSet
	completion slash.Completion

	queuedTurn  turn.Queue
	currentTurn Turn
}

const historyLimit = 1000

func (self *Harness) begin(message string) {
	self.settleMode()
	defer self.settleMode()

	history := edit.NewHistory(historyPath(), historyLimit)
	editor := edit.NewInput(history)
	self.editor = editor

	restoreTTY, err := tty.Raw(os.Stdin, os.Stdout)
	if err != nil {
		self.plainly(history, message)
		return
	}

	defer restoreTTY()

	restoreTerminal := self.terminal.Begin(self.mode.Current())
	defer restoreTerminal()

	defer func() { self.screen.Release(self.recorder.Stored()) }()

	self.show(editor)

	if message != "" {
		history.Add(message)
		if self.handleSlashCommand(message) == ordinaryInput {
			self.start(message)
		}
	}

	interaction.Run(os.Stdin, self.segmentLayout.RefreshInterval(), self.segmentLayout.IdleRefreshInterval(), interaction.Handler{
		Events: func() <-chan turn.Event { return self.currentTurn.Events() },
		Key:    func(keypress key.Key) bool { return self.apply(editor, history, keypress) },
		Turn:   self.takeTurn,
		TurnFinished: func() bool {
			self.finish()
			return true
		},
		Resize:  self.redraw,
		Running: func() bool { return self.currentTurn.Running() },
		Tick:    func() { self.currentTurn.spinnerFrame++ },
		Draw:    func() { self.show(editor) },
	})
}

func (self *Harness) apply(editor *edit.Input, history *edit.History, keypress key.Key) bool {
	switch keypress.Code {
	case key.FocusIn:
		return true
	case key.FocusOut:
		return true
	}

	action := editor.Apply(keypress, self.currentTurn.Running())
	if action != edit.Complete {
		self.completion.Reset()
	}

	switch action {
	case edit.Accept:
		self.acceptInput(editor, history)

	case edit.ForceAccept:
		self.submitInput(editor, history, strings.TrimSpace(editor.Text()))

	case edit.Continue:
		self.submitInput(editor, history, self.getOnWithItMessage)

	case edit.Cancel:
		self.cancelTurn()

	case edit.Quit:
		return false

	case edit.Complete:
		if completion, found := self.completion.Next(self.commands, editor.Text()); found {
			editor.SetText(completion)
		}

	case edit.ToggleWrite:
		self.toggleCap(caps.Write)

	case edit.ToggleShell:
		self.toggleCap(caps.Shell)

	case edit.ToggleGit:
		self.toggleCap(caps.Git)

	case edit.ToggleBackground:
		if self.mode.Current().Has(caps.Background) {
			names, err := self.processes.Disable()
			if err == nil {
				self.toggleCap(caps.Background)
				if len(names) > 0 {
					self.notifyStopped("Background processes killed (" + strings.Join(names, ", ") + ")")
				}
			} else {
				self.processes.Enable()
			}
		} else {
			self.processes.Enable()
			self.toggleCap(caps.Background)
		}

	case edit.Draw:
	}

	return true
}

type slashInput int

const (
	ordinaryInput slashInput = iota
	handledCommand
	unknownCommand
)

func (self *Harness) handleSlashCommand(message string) slashInput {
	invocation, found := self.commands.Find(message)
	if found {
		if err := invocation.Command.Run(commandContext{harness: self}, invocation.Arguments); err != nil {
			self.notifyFailure(slash.FormatError(invocation.Command.Name, err))
		}
		return handledCommand
	}

	name, isCommand := slash.CommandName(message)
	if !isCommand {
		return ordinaryInput
	}

	self.notifyFailure(fmt.Sprintf("Command not found: %s; press alt+enter to send anyway", name))
	return unknownCommand
}

type commandContext struct {
	harness *Harness
}

func (self commandContext) Emit(event agent.Event) {
	self.harness.notify(event)
}

func (self commandContext) Send(message string) {
	if self.harness.currentTurn.Running() {
		self.harness.replaceTurn(message)
	} else {
		self.harness.start(message)
	}
}

func (self commandContext) Notice(message string) {
	self.Emit(agent.Event{Kind: agent.HarnessMessageEvent, Text: message, Status: agent.InfoStatus})
}

func (self commandContext) Success(message string) {
	self.Emit(agent.Event{Kind: agent.HarnessMessageEvent, Text: message, Status: agent.SuccessStatus})
}

func (self *Harness) acceptInput(editor *edit.Input, history *edit.History) {
	message := strings.TrimSpace(editor.Text())
	switch self.handleSlashCommand(message) {
	case handledCommand:
		history.Add(message)
		editor.Reset()
	case ordinaryInput:
		self.submitInput(editor, history, message)
	}
}

func (self *Harness) submitInput(editor *edit.Input, history *edit.History, message string) {
	history.Add(message)
	editor.Reset()

	if message == "" {
		return
	}

	if self.currentTurn.Running() {
		self.replaceTurn(message)
	} else {
		self.start(message)
	}
}

func (self *Harness) toggleCap(whichCaps caps.Set) {
	self.mode.Toggle(whichCaps)
	self.terminal.SetMode(self.mode.Current())

	if i, isPending := self.pendingModeChange(whichCaps); isPending {
		self.takeBackModeChange(i, whichCaps)
	} else {
		self.showModeChange(whichCaps)
	}

	if self.currentTurn.Running() {
		self.queuedTurn.MarkModeChange()
		self.interruptTurn()
	}
}

func (self *Harness) pendingModeChange(whichCaps caps.Set) (int, bool) {
	for _, index := range self.pendingModeChanges {
		if self.events[index].Name == whichCaps.Flag() {
			return index, true
		}
	}

	return 0, false
}

func (self *Harness) showModeChange(whichCaps caps.Set) {
	event := caps.ModeToggleEvent(whichCaps, self.mode.Current())

	self.events = append(self.events, event)
	eventIndex := len(self.events) - 1
	self.pendingModeChanges = append(self.pendingModeChanges, eventIndex)
	self.noticePainter().DrawEvent(event)
}

func (self *Harness) takeBackModeChange(i int, whichCaps caps.Set) {
	self.events = slices.Delete(self.events, i, i+1)

	pendingModeChanges := make([]int, 0, len(self.pendingModeChanges)-1)

	for _, other := range self.pendingModeChanges {
		if other < i {
			pendingModeChanges = append(pendingModeChanges, other)
		} else if other > i {
			self.events[other-1] = caps.ModeWithout(self.events[other-1], whichCaps)
			pendingModeChanges = append(pendingModeChanges, other-1)
		}
	}

	self.pendingModeChanges = pendingModeChanges

	self.redraw()
}

func (self *Harness) settleMode() {
	if self.settledCaps == 0 {
		self.settledCaps = self.mode.Current()
		self.recordModeEvent(caps.ModeEvent(self.settledCaps))

		return
	}

	for _, index := range self.pendingModeChanges {
		self.recordModeEvent(self.events[index])
	}

	self.pendingModeChanges = nil
	self.settledCaps = self.mode.Current()
}

func (self *Harness) recordModeEvent(event agent.Event) {
	if err := self.recorder.Event(event); err != nil {
		self.notifyFailure("The conversation could not be stored: " + err.Error())
	}
	self.showStorageWarnings()
}

func (self *Harness) cancelTurn() {
	if self.currentTurn.Cancelled() {
		self.queuedTurn.Clear()
	}

	self.interruptTurn()
}

func (self *Harness) replaceTurn(message string) {
	self.queuedTurn.Replace(message)
	self.interruptTurn()
}

func (self *Harness) interruptTurn() {
	self.currentTurn.Interrupt()
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
	return self.currentTurn.Running(), self.currentTurn.spinnerFrame
}

func (self *Harness) isTurnRunning() bool {
	return self.currentTurn.Running()
}

func (self *Harness) turnElapsed() (bool, time.Duration, bool) {
	return self.currentTurn.Elapsed()
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

func (self *Harness) isPrefixPending() bool {
	return self.editor != nil && self.editor.IsPrefixPending()
}

func (self *Harness) plainly(history *edit.History, initialMessage string) {
	if initialMessage != "" {
		if self.handleSlashCommand(initialMessage) == ordinaryInput {
			self.ask(history, initialMessage)
		} else {
			history.Add(initialMessage)
		}
	}

	reader := bufio.NewScanner(os.Stdin)

	for reader.Scan() {
		stdin := strings.TrimSpace(reader.Text())
		if self.handleSlashCommand(stdin) == ordinaryInput {
			if stdin != "" {
				self.ask(history, stdin)
			}
		} else {
			history.Add(stdin)
		}
	}

	if err := reader.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "could not read input:", err)
	}
}

func (self *Harness) ask(history *edit.History, message string) {
	history.Add(message)
	self.start(message)

	for event := range self.currentTurn.Events() {
		self.takeTurn(event)
	}

	self.finish()
}

func (self *Harness) restore(storedSession *store.Session) {
	self.settledCaps, _ = caps.LastRecordedMode(storedSession.Events)

	if err := self.agent.RestoreState(storedSession.Events); err != nil {
		self.notifyFailure("The state could not be restored: " + err.Error())
		return
	}
	if err := self.agent.Load(storedSession.Items); err != nil {
		self.notifyFailure("The conversation could not be restored: " + err.Error())
		return
	}

	self.recorder.Resume(len(storedSession.Items))
	self.events = append(self.events, storedSession.Events...)

	self.metrics.Restore(storedSession.Events)

	self.screen.Reset()
	self.replay()
}

func (self *Harness) newPainter(isRunning bool) *painter.Painter {
	return &painter.Painter{
		Screen:       self.screen,
		IsRunning:    isRunning,
		GetTool:      self.agent.Tool,
		WorkspaceDir: self.workspaceDir,
	}
}

func (self *Harness) replay() {
	self.screen.Synchronise(func() {
		painter := self.newPainter(self.currentTurn.Running())

		for _, event := range self.events {
			painter.DrawEvent(event)
		}

		if self.currentTurn.Running() {
			self.currentTurn.painter = painter
			return
		}

		painter.Close(dynamic.Cancelled)

		self.screen.End()
	})
}

func (self *Harness) redraw() {
	var provisional agent.Delta
	var previousPainter *painter.Painter
	if self.currentTurn.Running() {
		previousPainter = self.currentTurn.painter
		provisional = previousPainter.ProvisionalDelta()
		previousPainter.Stop()
	}

	self.screen.Synchronise(func() {
		self.screen.Reset()
		self.replay()
		if provisional.Text != "" {
			self.currentTurn.painter.DrawRestoredDelta(provisional, previousPainter)
		}
	})
}

func (self *Harness) start(message string) {
	self.settleMode()
	self.metrics.BeginTurn()

	if fyi := self.mode.Inject(); fyi != "" {
		self.agent.FYI(fyi)
	}

	self.currentTurn = Turn{
		painter: self.newPainter(true),
		Stream:  turn.Start(self.agent, message),
	}

	self.screen.ReportProgress(true)
}

func (self *Harness) takeTurn(turnEvent TurnEvent) {
	if !self.currentTurn.Observe(turnEvent) {
		return
	}

	if turnEvent.Update.Delta != nil {
		self.metrics.RecordDelta(*turnEvent.Update.Delta)
		self.currentTurn.painter.DrawDelta(*turnEvent.Update.Delta)
		if self.currentTurn.painter.Stale() {
			self.redraw()
		}
		return
	}

	if turnEvent.Update.Event != nil {
		self.recordEvent(*turnEvent.Update.Event)
	}
}

func (self *Harness) recordEvent(event agent.Event) {
	self.metrics.Record(event)

	self.events = append(self.events, event)
	self.currentTurn.painter.DrawEvent(event)

	if self.currentTurn.painter.Stale() {
		self.redraw()
	}

	if err := self.recorder.Event(event); err != nil {
		self.notifyFailure("The conversation could not be stored: " + err.Error())
	}
	self.showStorageWarnings()
}

func (self *Harness) notifyFailure(text string) {
	self.notify(agent.Event{Kind: agent.HarnessMessageEvent, Text: text, Status: agent.ErrorStatus})
}

func (self *Harness) notifyStopped(text string) {
	self.notify(agent.Event{Kind: agent.HarnessMessageEvent, Text: text, Status: agent.WarningStatus})
}

func (self *Harness) notify(event agent.Event) {
	self.events = append(self.events, event)
	self.noticePainter().DrawEvent(event)

	_ = self.recorder.Event(event)
}

func (self *Harness) noticePainter() *painter.Painter {
	if self.currentTurn.Running() {
		return self.currentTurn.painter
	}

	return self.newPainter(false)
}

func (self *Harness) finish() {
	self.currentTurn.MarkFinished(time.Now())
	self.screen.ReportProgress(false)

	if self.currentTurn.Cancelled() {
		self.recordEvent(agent.Event{Kind: agent.InterruptionEvent})
	} else if self.currentTurn.Error() != nil {
		self.recordEvent(agent.Event{Kind: agent.FailureEvent, Text: self.currentTurn.Error().Error()})
	}

	if self.storeProviderState() {
		if err := self.recorder.CompleteTurn(); err != nil {
			self.notifyFailure("The turn completion could not be stored: " + err.Error())
		}
	}
	self.showStorageWarnings()
	self.currentTurn.painter.Close(dynamic.Cancelled)
	if self.currentTurn.painter.Stale() {
		self.redraw()
	}
	self.screen.End()

	self.metrics.FinishTurn()

	self.currentTurn.Finish()

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
		self.notifyFailure("The conversation state could not be stored: " + err.Error())
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
