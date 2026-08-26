package main

import (
	"bufio"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/bar"
	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/cycle"
	"crdx.org/io/cmd/oh/dispatch"
	"crdx.org/io/cmd/oh/dynamic"
	"crdx.org/io/cmd/oh/edit"
	"crdx.org/io/cmd/oh/input"
	"crdx.org/io/cmd/oh/interaction"
	"crdx.org/io/cmd/oh/key"
	"crdx.org/io/cmd/oh/location"
	"crdx.org/io/cmd/oh/metrics"
	"crdx.org/io/cmd/oh/output"
	"crdx.org/io/cmd/oh/painter"
	"crdx.org/io/cmd/oh/record"
	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/slash"
	"crdx.org/io/cmd/oh/store"
	"crdx.org/io/cmd/oh/terminal"
	"crdx.org/io/cmd/oh/tty"
	"crdx.org/io/cmd/oh/turn"
	"crdx.org/io/internal/sandbox"
	"crdx.org/io/internal/stop"
)

type SessionLogger = record.Session

type App struct {
	agent              *agent.Agent
	events             []agent.Event
	screen             *output.Screen
	recorder           *record.Recorder
	processes          *sandbox.Processes
	segmentLayout      segment.Layout
	editor             *edit.Input
	mode               *caps.Mode
	settledCaps        caps.Set
	pendingModeChanges []agent.Event
	modeNotices        *painter.ModeNotices
	modeNoticeBlock    *output.BlockHandle
	terminal           terminal.Terminal
	metrics            metrics.Tracker

	workspaceDir       string
	getOnWithItMessage string

	commands   slash.Registry
	completion slash.Completion

	transition  cycle.Transition
	queuedTurn  turn.Queue
	currentTurn Turn
}

type Turn struct {
	*turn.Stream

	spinnerFrame int
	painter      *painter.Picasso
}

type TurnEvent = turn.Event

const historyLimit = 1000

func (self *App) begin(message string) cycle.Transition {
	self.settleMode()
	defer self.settleMode()

	history := edit.NewHistory(location.GetHistoryPath(), historyLimit)
	editor := edit.NewInput(history)
	self.editor = editor

	restoreTTY, err := tty.Raw(os.Stdin, os.Stdout)
	if err != nil {
		self.plainly(history, message)
		return self.transition
	}

	defer restoreTTY()

	restoreTerminal := self.terminal.Begin(self.mode.Current())
	defer restoreTerminal()

	defer func() { self.screen.Release(self.recorder.IsPersisted()) }()

	self.show(editor)

	if message != "" {
		history.Add(message)
		if self.handleCommand(message) == dispatch.Ordinary {
			self.start(message)
		}
	}
	if self.isTransitionRequested() {
		return self.transition
	}

	interaction.Run(os.Stdin, self.segmentLayout.RefreshInterval(), self.segmentLayout.IdleRefreshInterval, interaction.Handler{
		Events: func() <-chan turn.Event { return self.currentTurn.Events() },
		Key:    func(keypress key.Key) bool { return self.handleKeypressAndShowInput(editor, history, keypress) },
		Turn:   self.takeTurn,
		TurnFinished: func() bool {
			self.finish()
			return !self.isTransitionRequested()
		},
		Resize:  self.redraw,
		Running: func() bool { return self.currentTurn.Running() },
		Tick:    func() { self.currentTurn.spinnerFrame++ },
		Draw:    func() { self.show(editor) },
	})

	return self.transition
}

func (self *App) handleKeypressAndShowInput(editor *edit.Input, history *edit.History, keypress key.Key) bool {
	shouldContinue := true
	self.screen.Sync(func() {
		shouldContinue = self.apply(editor, history, keypress)
		if shouldContinue {
			self.show(editor)
		}
	})
	return shouldContinue
}

func (self *App) apply(editor *edit.Input, history *edit.History, keypress key.Key) bool {
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

	case edit.ToggleWeb:
		self.toggleCap(caps.Web)

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

	return !self.isTransitionRequested() || self.currentTurn.Running()
}

func (self *App) isTransitionRequested() bool {
	return self.transition.Kind != cycle.Quit
}

func (self *App) requestTransition(transition cycle.Transition) error {
	self.transition = transition
	if self.currentTurn.Running() {
		self.interruptTurn(transitionReason)
	}
	return nil
}

func (self *App) handleCommand(message string) dispatch.Result {
	result, failure := dispatch.Handle(self.commands, dispatch.Actions{
		EmitEvent:  self.notify,
		SendPrompt: self.sendCommandPrompt,
	}, message)
	if failure != "" {
		self.notifyFailure(failure)
	}
	return result
}

func (self *App) sendCommandPrompt(message string) {
	if self.currentTurn.Running() {
		self.replaceTurn(message)
		return
	}
	self.start(message)
}

func (self *App) acceptInput(editor *edit.Input, history *edit.History) {
	message := strings.TrimSpace(editor.Text())
	switch self.handleCommand(message) {
	case dispatch.Handled:
		history.Add(message)
		editor.Reset()
	case dispatch.Ordinary:
		self.submitInput(editor, history, message)
	}
}

func (self *App) submitInput(editor *edit.Input, history *edit.History, message string) {
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

func (self *App) toggleCap(whichCaps caps.Set) {
	self.mode.Toggle(whichCaps)
	self.terminal.SetMode(self.mode.Current())

	if i, isPending := self.pendingModeChange(whichCaps); isPending {
		self.takeBackModeChange(i, whichCaps)
	} else {
		self.showModeChange(whichCaps)
	}

	if self.currentTurn.Running() {
		self.queuedTurn.MarkModeChange()
		self.interruptTurn(modeReason)
	}
}

func (self *App) pendingModeChange(whichCaps caps.Set) (int, bool) {
	for index, event := range self.pendingModeChanges {
		if event.Name == whichCaps.Flag() {
			return index, true
		}
	}

	return 0, false
}

func (self *App) showModeChange(whichCaps caps.Set) {
	event := caps.ModeToggleEvent(whichCaps, self.mode.Current())
	self.pendingModeChanges = append(self.pendingModeChanges, event)

	if self.currentTurn.Running() {
		self.noticePainter().DrawEvent(event)
		return
	}

	self.refreshPendingModeNotices()
}

func (self *App) takeBackModeChange(index int, whichCaps caps.Set) {
	self.pendingModeChanges = slices.Delete(self.pendingModeChanges, index, index+1)

	for other := index; other < len(self.pendingModeChanges); other++ {
		self.pendingModeChanges[other] = caps.ModeWithout(self.pendingModeChanges[other], whichCaps)
	}

	if self.currentTurn.Running() {
		self.redraw()
		return
	}

	self.refreshPendingModeNotices()
}

func (self *App) refreshPendingModeNotices() {
	if len(self.pendingModeChanges) == 0 {
		handle := self.modeNoticeBlock
		self.modeNotices = nil
		self.modeNoticeBlock = nil

		if handle != nil && !self.screen.DiscardBlock(handle) {
			self.redraw()
		}
		return
	}

	if self.modeNotices == nil {
		self.modeNotices = painter.NewModeNotices(self.pendingModeChanges)
		self.modeNoticeBlock = self.screen.OpenNotice(self.modeNotices)
		return
	}

	self.modeNotices.ReplaceEvents(self.pendingModeChanges)
	if !self.screen.RefreshBlock(self.modeNoticeBlock) {
		self.modeNotices = nil
		self.modeNoticeBlock = nil
		self.redraw()
	}
}

func (self *App) settleMode() {
	if self.settledCaps == 0 {
		self.settledCaps = self.mode.Current()
		self.recordModeEvent(caps.ModeEvent(self.settledCaps))

		return
	}

	for _, event := range self.pendingModeChanges {
		self.events = append(self.events, event)
		self.recordModeEvent(event)
	}

	if self.modeNoticeBlock != nil {
		self.screen.SealBlock(self.modeNoticeBlock)
	}
	self.pendingModeChanges = nil
	self.modeNotices = nil
	self.modeNoticeBlock = nil
	self.settledCaps = self.mode.Current()
}

func (self *App) recordModeEvent(event agent.Event) {
	if err := self.recorder.Event(event); err != nil {
		self.notifyFailure("The conversation could not be stored: " + err.Error())
	}
	self.showStorageWarnings()
}

const (
	escapeReason     = "the user pressed escape"
	replacedReason   = "the user sent another message"
	modeReason       = "the user changed what the harness is allowed to do"
	transitionReason = "the session is being closed"
)

func (self *App) cancelTurn() {
	if self.currentTurn.Cancelled() {
		self.queuedTurn.Clear()
	}

	self.interruptTurn(escapeReason)
}

func (self *App) replaceTurn(message string) {
	self.queuedTurn.Replace(message)
	self.interruptTurn(replacedReason)
}

func (self *App) interruptTurn(reason string) {
	self.currentTurn.Interrupt(stop.Because(reason))
}

func (self *App) show(editor *edit.Input) {
	if editor.IsPasting() {
		return
	}

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

func (self *App) bar(position segment.Position, frame edit.Frame) string {
	return bar.Render(self.segmentLayout, position, segment.Context{
		HiddenLinesAbove: frame.HiddenLinesAbove,
		HiddenLinesBelow: frame.HiddenLinesBelow,
	})
}

func (self *App) getBarSources() bar.Sources {
	return bar.Sources{
		GetTurnActivity:      self.turnActivity,
		GetContextUsage:      self.contextUsage,
		GetGrantedCaps:       self.grantedCaps,
		IsPrefixPending:      self.isPrefixPending,
		GetTurnElapsed:       self.turnElapsed,
		GetTurnCount:         self.turnCount,
		GetLastTurnTokenRate: self.lastTurnTokenRate,
		IsTurnRunning:        self.isTurnRunning,
	}
}

func (self *App) turnActivity() (bool, int) {
	return self.currentTurn.Running(), self.currentTurn.spinnerFrame
}

func (self *App) isTurnRunning() bool {
	return self.currentTurn.Running()
}

func (self *App) turnElapsed() (bool, time.Duration, bool) {
	return self.currentTurn.Elapsed()
}

func (self *App) turnCount() int {
	return self.metrics.TurnCount()
}

func (self *App) lastTurnTokenRate() (float64, bool) {
	return self.metrics.LastTurnTokenRate()
}

func (self *App) contextUsage() (int, int) {
	return self.metrics.ContextUsage()
}

func (self *App) grantedCaps() caps.Set {
	return self.mode.Current()
}

func (self *App) isPrefixPending() bool {
	return self.editor != nil && self.editor.IsPrefixPending()
}

func (self *App) plainly(history *edit.History, initialMessage string) {
	self.acceptPlainInput(history, initialMessage)
	if self.isTransitionRequested() {
		return
	}

	reader := bufio.NewScanner(os.Stdin)

	for reader.Scan() {
		self.acceptPlainInput(history, strings.TrimSpace(reader.Text()))
		if self.isTransitionRequested() {
			return
		}
	}

	if err := reader.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "could not read input:", err)
	}
}

func (self *App) acceptPlainInput(history *edit.History, message string) {
	if message == "" {
		return
	}
	if self.handleCommand(message) == dispatch.Ordinary {
		self.ask(history, message)
		return
	}

	history.Add(message)
	if self.currentTurn.Running() {
		self.waitForCurrentTurn()
	}
}

func (self *App) ask(history *edit.History, message string) {
	history.Add(message)
	self.start(message)
	self.waitForCurrentTurn()
}

func (self *App) waitForCurrentTurn() {
	for event := range self.currentTurn.Events() {
		self.takeTurn(event)
	}
	self.finish()
}

func (self *App) getLastMessage() (string, bool) {
	for _, event := range slices.Backward(self.events) {
		if event.Kind == agent.ModelMessageEvent {
			return event.Text, true
		}
	}
	return "", false
}

func (self *App) restore(storedSession *store.Session) {
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

func (self *App) newPainter(isRunning bool) *painter.Picasso {
	return painter.New(self.screen, isRunning, self.agent.Tool, self.workspaceDir)
}

func (self *App) replay() {
	self.screen.Sync(func() {
		painter := self.newPainter(self.currentTurn.Running())

		for _, event := range self.events {
			painter.DrawEvent(event)
		}

		if self.currentTurn.Running() {
			self.currentTurn.painter = painter
			self.refreshPendingModeNotices()
			return
		}

		painter.Close(dynamic.Cancelled)

		self.screen.End()
		self.refreshPendingModeNotices()
	})
}

func (self *App) redraw() {
	var provisionalPainter agent.Delta
	var previousPainter *painter.Picasso
	if self.currentTurn.Running() {
		previousPainter = self.currentTurn.painter
		provisionalPainter = previousPainter.ProvisionalDelta()
		previousPainter.Stop()
	}

	self.screen.Sync(func() {
		self.modeNotices = nil
		self.modeNoticeBlock = nil
		self.screen.Reset()
		self.replay()
		if provisionalPainter.Text != "" {
			self.currentTurn.painter.DrawRestoredDelta(provisionalPainter, previousPainter)
		}
	})
}

func (self *App) start(message string) {
	self.settleMode()
	self.metrics.BeginTurn()

	notes := slices.DeleteFunc(
		[]string{self.interruptionNote(), self.mode.Inject()},
		func(note string) bool { return note == "" },
	)
	if len(notes) > 0 {
		self.agent.FYI(strings.Join(notes, " "))
	}

	self.currentTurn = Turn{
		painter: self.newPainter(true),
		Stream:  turn.Start(self.agent, message),
	}

	self.screen.ReportProgress(true)
}

func (self *App) interruptionNote() string {
	if !self.currentTurn.Cancelled() {
		return ""
	}

	note := "The previous turn was stopped before it finished"
	if reason := self.currentTurn.Reason(); reason != nil {
		note += " because " + reason.Error()
	}

	return note + "."
}

func (self *App) takeTurn(turnEvent TurnEvent) {
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

func (self *App) recordEvent(event agent.Event) {
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

func (self *App) notifyFailure(text string) {
	self.notify(agent.Event{Kind: agent.HarnessMessageEvent, Text: text, Status: agent.ErrorStatus})
}

func (self *App) notifyStopped(text string) {
	self.notify(agent.Event{Kind: agent.HarnessMessageEvent, Text: text, Status: agent.WarningStatus})
}

func (self *App) notify(event agent.Event) {
	self.events = append(self.events, event)
	self.noticePainter().DrawEvent(event)

	_ = self.recorder.Event(event)
}

func (self *App) noticePainter() *painter.Picasso {
	if self.currentTurn.Running() {
		return self.currentTurn.painter
	}

	return self.newPainter(false)
}

func (self *App) finish() {
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

func (self *App) storeProviderState() bool {
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

func (self *App) showStorageWarnings() {
	for _, err := range self.recorder.TakeWarnings() {
		self.notifyFailure(err.Error())
	}
}
