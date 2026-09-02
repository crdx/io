package main

import (
	"bufio"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/access"
	"crdx.org/io/cmd/oh/bar"
	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/config"
	"crdx.org/io/cmd/oh/cycle"
	"crdx.org/io/cmd/oh/dispatch"
	"crdx.org/io/cmd/oh/dynamic"
	"crdx.org/io/cmd/oh/edit"
	"crdx.org/io/cmd/oh/editor"
	"crdx.org/io/cmd/oh/input"
	"crdx.org/io/cmd/oh/interaction"
	"crdx.org/io/cmd/oh/key"
	"crdx.org/io/cmd/oh/location"
	"crdx.org/io/cmd/oh/metrics"
	"crdx.org/io/cmd/oh/output"
	"crdx.org/io/cmd/oh/painter"
	"crdx.org/io/cmd/oh/pathgrant"
	"crdx.org/io/cmd/oh/record"
	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/slash"
	"crdx.org/io/cmd/oh/store"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/oh/terminal"
	"crdx.org/io/cmd/oh/tty"
	"crdx.org/io/cmd/oh/turn"
	"crdx.org/io/cmd/oh/width"
	"crdx.org/io/cmd/oh/work"
	"crdx.org/io/internal/stop"
	"crdx.org/io/tool/middleware/truncate"
	"crdx.org/io/toolbox/title"
)

type SessionLogger = record.Session

type pendingMessage struct {
	message agent.Event
	state   agent.Event
}

type pendingInput struct {
	items    []pendingMessage
	renderer *painter.PendingMessages
	block    *output.BlockHandle
}

type feedbackSource int

const (
	systemFeedback feedbackSource = iota
	commandFeedback
	configFeedback
	confirmationFeedback
)

func (self feedbackSource) isDismissedByTyping() bool {
	switch self {
	case commandFeedback, confirmationFeedback:
		return true
	case systemFeedback, configFeedback:
		return false
	default:
		return false
	}
}

type feedbackMessage struct {
	text   string
	status agent.Status
}

type feedbackState struct {
	source  feedbackSource
	message feedbackMessage
}

func (self *pendingInput) add(message agent.Event, state agent.Event) {
	self.items = append(self.items, pendingMessage{message: message, state: state})
}

func (self *pendingInput) takeBack(index int) {
	self.items = slices.Delete(self.items, index, index+1)
}

func (self *pendingInput) texts() []string {
	texts := make([]string, len(self.items))
	for i, item := range self.items {
		texts[i] = item.message.Text
	}
	return texts
}

type App struct {
	agent               *agent.Agent
	events              []agent.Event
	openingEvents       []agent.Event
	screen              *output.Screen
	recorder            *record.Recorder
	barConfiguration    bar.Configuration
	configObserver      *config.Observer
	inputLine           *edit.Input
	editorConfiguration *editor.Configuration
	mode                *caps.Mode
	pathGrants          *pathgrant.Grants
	settledCaps         caps.Set
	pending             pendingInput
	feedback            feedbackState
	terminal            terminal.Terminal
	metrics             metrics.Tracker
	toolOutputLimit     *truncate.Limit
	onFailure           func(failure error)

	workspace       *work.Space
	continueMessage string
	streamingMode   output.StreamingMode

	commands   slash.Registry
	completion slash.Completion

	transition  cycle.Transition
	queuedTurn  turn.Queue
	currentTurn Turn
	startedAt   time.Time
	isPlain     bool
	isYolo      bool
}

type Turn struct {
	*turn.Stream

	painter *painter.Picasso
}

type TurnEvent = turn.Event

const historyLimit = 1000

func (self *App) begin(message string) cycle.Transition {
	self.initialiseAccess()

	history := edit.NewHistory(location.GetHistoryPath(), historyLimit)
	inputLine := edit.NewInput(history)
	self.inputLine = inputLine

	restoreTTY, err := tty.Raw(os.Stdin, os.Stdout)
	if err != nil {
		self.plainly(history, message)
		return self.transition
	}

	restoreTitle := self.terminal.Begin(self.mode.Current())
	restoreCursor := self.screen.BeginEditing()

	restoreTerminal := func() {
		restoreTerminalState(
			self.screen,
			self.recorder.IsPersisted(),
			restoreCursor,
			restoreTitle,
			restoreTTY,
		)
	}

	defer restoreTerminal()

	stopListening := tty.RestoreOnSignal(restoreTerminal)
	defer stopListening()

	defer self.dropPendingInput()

	self.show(inputLine)
	if len(self.pending.items) > 0 {
		self.refreshPendingMessages()
	}

	if message != "" {
		history.Add(message)
		if self.handleCommand(message) == dispatch.Ordinary {
			self.start(message)
		}
	}
	if self.isTransitionRequested() {
		return self.transition
	}

	interaction.Run(os.Stdin, self.nextBarRefresh, interaction.Handler{
		Events: func() <-chan turn.Event { return self.currentTurn.Events() },
		Key:    func(keypress key.Key) bool { return self.handleKeypressAndShowInput(inputLine, history, keypress) },
		Turn:   self.takeTurn,
		TurnFinished: func() bool {
			self.finish()
			return !self.isTransitionRequested()
		},
		Resize:  self.redraw,
		Beat:    self.screen.RefreshProgress,
		Changes: self.configObserver.Changes(),
		Change:  self.reloadConfig,
		Draw:    func() { self.show(inputLine) },
	})

	return self.transition
}

func restoreTerminalState(screen *output.Screen, isPersisted bool, restorers ...func()) {
	screen.Release(isPersisted)
	for _, restore := range restorers {
		restore()
	}
}

func (self *App) handleKeypressAndShowInput(inputLine *edit.Input, history *edit.History, keypress key.Key) bool {
	shouldContinue := true
	self.screen.Sync(func() {
		shouldContinue = self.apply(inputLine, history, keypress)
		if shouldContinue {
			self.show(inputLine)
		}
	})
	return shouldContinue
}

func (self *App) apply(inputLine *edit.Input, history *edit.History, keypress key.Key) bool {
	if keypress.Code == key.FocusIn || keypress.Code == key.FocusOut {
		return true
	}

	previousText := inputLine.Text()
	action := inputLine.Apply(keypress, self.currentTurn.Running())
	if inputLine.Text() != previousText {
		self.clearFeedbackOnTyping()
	}
	if action != edit.Complete {
		self.completion.Reset()
	}

	switch action {
	case edit.Accept:
		self.acceptInput(inputLine, history)

	case edit.ForceAccept:
		self.submitInput(inputLine, history, strings.TrimSpace(inputLine.Text()))

	case edit.Continue:
		self.continueOrFlush(inputLine, history)

	case edit.Cancel:
		if !self.takeBackInterjection(inputLine) {
			self.cancelTurn(stopKeyReason(keypress))
		}

	case edit.Quit:
		return false

	case edit.Complete:
		if completion, found := self.completion.Next(self.commands, inputLine.Text()); found {
			inputLine.SetText(completion)
		}

	case edit.ToggleWrite:
		self.toggleCap(caps.Write)

	case edit.ToggleShell:
		self.toggleCap(caps.Shell)

	case edit.ToggleGit:
		self.toggleCap(caps.Git)

	case edit.ToggleWeb:
		self.toggleCap(caps.Web)

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
	self.clearFeedback(commandFeedback)
	result, failure := dispatch.Handle(self.commands, dispatch.Actions{
		EmitEvent:  self.emitCommandEvent,
		SendPrompt: self.sendCommandPrompt,
		ShowFeedback: func(text string, status agent.Status) {
			self.showFeedback(commandFeedback, feedbackMessage{text: text, status: status})
		},
	}, message)
	if failure != "" {
		self.showFeedback(commandFeedback, feedbackMessage{text: failure, status: agent.ErrorStatus})
	}
	return result
}

func (self *App) emitCommandEvent(event agent.Event) {
	if event.Kind != pathgrant.Change {
		self.notify(event)
		return
	}

	self.queuePathGrantChange(event)
	if self.currentTurn.Running() {
		self.queuedTurn.MarkAccessChange()
		self.interruptTurn(accessReason)
		return
	}
	self.refreshPendingMessages()
}

func (self *App) queuePathGrantChange(event agent.Event) {
	if index, isPending := self.pendingPathGrantChange(event.Name); isPending {
		self.takeBackPathGrantChange(index, event.Name)

		if self.pathGrants.IsTold(event.Name) {
			return
		}
	}

	message, isShown := pathgrant.Notice(event)
	if !isShown {
		return
	}
	self.pending.add(agent.Event{Kind: agent.UserMessageEvent, Text: message}, event)
}

func (self *App) pendingPathGrantChange(path string) (int, bool) {
	for index, item := range self.pending.items {
		if item.state.Kind == pathgrant.Change && item.state.Name == path {
			return index, true
		}
	}

	return 0, false
}

func (self *App) takeBackPathGrantChange(index int, path string) {
	self.pending.takeBack(index)

	grants := self.pathGrants.GetCurrent()
	for other := range self.pending.items {
		item := &self.pending.items[other]
		if item.state.Kind != pathgrant.Change {
			continue
		}
		item.state = pathgrant.WithGrantOf(item.state, path, grants)
	}
}

func (self *App) sendCommandPrompt(message string) {
	if self.currentTurn.Interject(message) {
		return
	}
	self.start(message)
}

func (self *App) acceptInput(inputLine *edit.Input, history *edit.History) {
	message := strings.TrimSpace(inputLine.Text())
	switch self.handleCommand(message) {
	case dispatch.Handled:
		history.Add(message)
		inputLine.Reset()
	case dispatch.Ordinary:
		self.submitInput(inputLine, history, message)
	case dispatch.Rejected:
	}
}

func (self *App) submitInput(inputLine *edit.Input, history *edit.History, message string) {
	history.Add(message)
	inputLine.Reset()

	if message == "" {
		return
	}

	if !self.currentTurn.Interject(message) {
		self.start(message)
	}
}

func (self *App) sendInput(inputLine *edit.Input, history *edit.History, message string) {
	history.Add(message)
	inputLine.Reset()

	if message == "" {
		return
	}

	if self.currentTurn.Running() {
		self.replaceTurn(message)
	} else {
		self.start(message)
	}
}

func (self *App) continueOrFlush(inputLine *edit.Input, history *edit.History) {
	if message, isQueued := self.currentTurn.TakeInterjections(); isQueued {
		self.replaceTurn(message)
		return
	}

	self.sendInput(inputLine, history, self.continueMessage)
}

func (self *App) takeBackInterjection(inputLine *edit.Input) bool {
	if inputLine == nil {
		return false
	}

	message, isQueued := self.currentTurn.TakeLastInterjection()
	if !isQueued {
		return false
	}

	if typedText := inputLine.Text(); typedText != "" {
		message += agent.InterjectionSeparator + typedText
	}

	inputLine.SetText(message)

	return true
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
		self.queuedTurn.MarkAccessChange()
		self.interruptTurn(accessReason)
	}
}

func (self *App) pendingModeChange(whichCaps caps.Set) (int, bool) {
	for index, item := range self.pending.items {
		if item.state.Name == whichCaps.Flag() {
			return index, true
		}
	}

	return 0, false
}

func (self *App) showModeChange(whichCaps caps.Set) {
	event := caps.ModeToggleEvent(whichCaps, self.mode.Current())
	text, _ := caps.ModeNotice(event)
	message := agent.Event{Kind: agent.UserMessageEvent, Text: text}
	self.pending.add(message, event)

	if !self.currentTurn.Running() {
		self.refreshPendingMessages()
	}
}

func (self *App) takeBackModeChange(index int, whichCaps caps.Set) {
	self.pending.takeBack(index)

	for other := index; other < len(self.pending.items); other++ {
		item := &self.pending.items[other]
		if item.state.Kind != caps.ModeChange {
			continue
		}
		item.state = caps.ModeWithout(item.state, whichCaps)
		item.message.Text, _ = caps.ModeNotice(item.state)
	}

	if self.currentTurn.Running() {
		self.redraw()
		return
	}

	self.refreshPendingMessages()
}

func (self *App) refreshPendingMessages() {
	if len(self.pending.items) == 0 {
		handle := self.pending.block
		self.pending.renderer = nil
		self.pending.block = nil

		if handle != nil && !self.screen.DiscardBlock(handle) {
			self.redraw()
		}
		return
	}

	messages := self.pending.texts()
	if self.pending.renderer == nil {
		self.pending.renderer = painter.NewPendingMessages(messages, self.screen.IsTerminal())
		self.screen.Blank()
		self.pending.block = self.screen.OpenNotice(self.pending.renderer)
		return
	}

	self.pending.renderer.Replace(messages)
	if !self.screen.RefreshBlock(self.pending.block) {
		self.pending.renderer = nil
		self.pending.block = nil
		self.redraw()
	}
}

func (self *App) initialiseAccess() {
	if self.settledCaps != 0 {
		return
	}
	self.settledCaps = self.mode.Current()
	self.recordModeEvent(caps.ModeEvent(self.settledCaps))
	for _, event := range self.openingEvents {
		self.storeEvent(event)
	}
	self.openingEvents = nil
}

func (self *App) settleAccess() {
	if self.settledCaps == 0 {
		self.initialiseAccess()
		return
	}

	self.settlePendingInput()
	self.settledCaps = self.mode.Current()
}

func (self *App) settlePendingInput() {
	wasShown := self.pending.block != nil
	for _, item := range self.pending.items {
		if item.state.Kind != "" {
			item.state.Name = ""
			self.events = append(self.events, item.state)
			self.storeEvent(item.state)
		}
		if wasShown {
			self.metrics.Record(item.message)
			self.events = append(self.events, item.message)
			self.storeEvent(item.message)
		}
	}

	if self.pending.block != nil {
		self.pending.renderer.MarkSent()
		self.screen.RefreshBlock(self.pending.block)
		self.screen.SealBlock(self.pending.block)
	}
	self.pending = pendingInput{}
}

func (self *App) dropPendingInput() {
	if self.pending.block != nil {
		self.screen.DiscardBlock(self.pending.block)
	}
	self.pending = pendingInput{}
}

func (self *App) recordModeEvent(event agent.Event) {
	self.storeEvent(event)
}

const (
	escapeReason     = "the user pressed escape"
	ctrlDReason      = "the user pressed ctrl+d"
	replacedReason   = "the user sent another message"
	accessReason     = "the user changed what the harness is allowed to do"
	transitionReason = "the session is being closed"
)

func stopKeyReason(keypress key.Key) string {
	if keypress.Code == key.Escape {
		return escapeReason
	}

	return ctrlDReason
}

func (self *App) cancelTurn(reason string) {
	if self.currentTurn.Cancelled() {
		self.queuedTurn.Drop()
	}

	self.interruptTurn(reason)
}

func (self *App) replaceTurn(message string) {
	self.queuedTurn.Replace(message)
	self.interruptTurn(replacedReason)
}

func (self *App) interruptTurn(reason string) {
	self.currentTurn.Interrupt(stop.Because(reason))
}

func (self *App) show(inputLine *edit.Input) {
	if inputLine.IsPasting() {
		return
	}

	columns := self.screen.Columns()
	frame := inputLine.Frame(columns)

	topRight := self.renderBar(segment.TopRight, frame)
	bottomRight := self.renderBar(segment.BottomRight, frame)
	block := input.Block{
		Top: input.Ruler{
			Left:   self.renderBarWithin(segment.TopLeft, frame, input.LeftContentWidth(columns, topRight)),
			Center: self.renderBar(segment.TopCenter, frame),
			Right:  topRight,
		},
		Input: frame,
		Bottom: input.Ruler{
			Left:   self.renderBarWithin(segment.BottomLeft, frame, input.LeftContentWidth(columns, bottomRight)),
			Center: self.renderBar(segment.BottomCenter, frame),
			Right:  bottomRight,
		},
		Status: self.statusRows(columns),
		Rule:   self.ruleStyle(),
	}

	self.screen.Footer(block.Rows(columns))
}

func (self *App) ruleStyle() style.Style {
	if self.isYolo {
		return style.Hazard
	}

	return style.Rule
}

func (self *App) statusRows(columns int) []string {
	if self.feedback.message.text == "" {
		return painter.RenderQueuedMessages(self.currentTurn.GetInterjections(), columns)
	}

	styledText := painter.NoticeStyle(self.feedback.message.status)(self.feedback.message.text)
	return width.Wrap(styledText, columns)
}

func (self *App) showFeedback(source feedbackSource, message feedbackMessage) {
	if self.isPlain {
		self.screen.Line(painter.NoticeStyle(message.status)(message.text))
		return
	}

	self.feedback = feedbackState{source: source, message: message}
}

func (self *App) clearFeedback(source feedbackSource) {
	if self.feedback.message.text != "" && self.feedback.source == source {
		self.feedback = feedbackState{}
	}
}

func (self *App) clearFeedbackOnTyping() {
	if self.feedback.source.isDismissedByTyping() {
		self.feedback = feedbackState{}
	}
}

func (self *App) renderBar(position segment.Position, frame edit.Frame) string {
	return self.barConfiguration.Render(position, getBarContext(frame))
}

func (self *App) renderBarWithin(position segment.Position, frame edit.Frame, cells int) string {
	return self.barConfiguration.RenderWithin(position, getBarContext(frame), cells)
}

func getBarContext(frame edit.Frame) segment.Context {
	return segment.Context{
		HiddenLinesAbove: frame.HiddenLinesAbove,
		HiddenLinesBelow: frame.HiddenLinesBelow,
	}
}

func (self *App) getBarSources() bar.Sources {
	return bar.Sources{
		IsTurnRunning:   self.isTurnRunning,
		GetContextUsage: self.contextUsage,
		GetGrantedCaps:  self.grantedCaps,
		GetPathGrants:   self.getPathGrants,
		IsPrefixPending: self.isPrefixPending,
		GetTurnTiming:   self.turnTiming,
		GetTurnCount:    self.turnCount,
	}
}

func (self *App) isTurnRunning() bool {
	return self.currentTurn.Running()
}

func (self *App) nextBarRefresh(at time.Time) time.Time {
	return self.barConfiguration.NextRefresh(segment.Phase{At: at, IsRunning: self.isTurnRunning()})
}

func (self *App) reloadConfig(watchFailure error) bool {
	result := self.configObserver.Reload(watchFailure, self.barConfiguration.GetRegistry())
	switch result.Status {
	case config.ReloadUnchanged:
		return false
	case config.ReloadFailed:
		self.showFeedback(configFeedback, feedbackMessage{
			text:   "The configuration could not be reloaded: " + result.Failure.Error(),
			status: agent.ErrorStatus,
		})
		return true
	case config.ReloadApplied:
		if err := self.commands.ReplaceCommandSet(result.LiveConfig.SnippetCommandSet); err != nil {
			self.showFeedback(configFeedback, feedbackMessage{
				text:   "The configuration could not be reloaded: could not replace snippets: " + err.Error(),
				status: agent.ErrorStatus,
			})
			return true
		}
		self.completion.Reset()
		self.continueMessage = result.LiveConfig.ContinueMessage
		self.editorConfiguration.ReplaceCommand(result.LiveConfig.EditorCommand)
		self.streamingMode = result.LiveConfig.StreamingMode
		self.toolOutputLimit.Replace(result.LiveConfig.ToolOutputBytes)
		self.barConfiguration.ReplaceLayout(result.LiveConfig.SegmentLayout)
		self.clearFeedback(configFeedback)
		if self.feedback.message.status != agent.ErrorStatus {
			self.showFeedback(confirmationFeedback, feedbackMessage{
				text:   "The configuration was reloaded automatically",
				status: agent.SuccessStatus,
			})
		}
	}
	return true
}

func (self *App) turnTiming() turn.Timing {
	if timing, isKnown := self.currentTurn.Timing(); isKnown {
		return timing
	}

	if self.startedAt.IsZero() {
		return turn.Timing{}
	}
	return turn.Timing{UserTurn: time.Since(self.startedAt)}
}

func (self *App) turnCount() int {
	return self.metrics.TurnCount()
}

func (self *App) contextUsage() (int, int) {
	return self.metrics.ContextUsage()
}

func (self *App) grantedCaps() caps.Set {
	return self.mode.Current()
}

func (self *App) getPathGrants() []pathgrant.Grant {
	if self.pathGrants == nil {
		return nil
	}
	return self.pathGrants.GetCurrent()
}

func (self *App) isPrefixPending() bool {
	return self.inputLine != nil && self.inputLine.IsPrefixPending()
}

func (self *App) plainly(history *edit.History, initialMessage string) {
	self.isPlain = true
	defer func() { self.isPlain = false }()

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
	for self.currentTurn.Running() {
		for event := range self.currentTurn.Events() {
			self.takeTurn(event)
		}
		self.finish()
	}
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

	for _, event := range storedSession.Events {
		self.takeSessionTitle(event)
	}

	self.metrics.Restore(storedSession.Events, storedSession.TurnCompletions)

	self.screen.Reset()
	self.replay()
}

func (self *App) newPainter(isRunning bool) *painter.Picasso {
	picasso := painter.New(self.screen, isRunning, self.agent.Tool, self.workspace, self.streamingMode)
	if self.screen.IsTerminal() && self.recorder != nil {
		picasso.LinkToolResults(self.recorder.Name())
	}
	return picasso
}

func (self *App) replay() {
	self.screen.Sync(func() {
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
		self.refreshPendingMessages()
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
		self.pending.renderer = nil
		self.pending.block = nil
		self.screen.Reset()
		self.replay()
		if provisionalPainter.Text != "" {
			self.currentTurn.painter.DrawRestoredDelta(provisionalPainter, previousPainter)
		}
	})
}

func (self *App) start(message string) {
	userTurnElapsed := self.turnTiming().UserTurn
	self.settleAccess()
	self.metrics.BeginTurn()

	if note := self.prelude(); note != "" {
		self.agent.AddUserMessage(note)
	}

	self.currentTurn = Turn{
		painter: self.newPainter(true),
		Stream: turn.Start(self.agent, message, turn.Timing{
			UserTurn: userTurnElapsed,
		}),
	}

	self.screen.ReportProgress(true)
}

func (self *App) takeSessionTitle(event agent.Event) {
	if sessionTitle, isTitle := agent.TitleFromEvent(event); isTitle {
		self.terminal.SetSessionTitle(sessionTitle)
	}
}

func (self *App) prelude() string {
	notes := slices.DeleteFunc(
		[]string{self.interruptionNote(), self.accessMessage(), self.titleNote()},
		func(note string) bool { return note == "" },
	)

	return strings.Join(notes, " ")
}

func (self *App) accessMessage() string {
	tellers := []access.Teller{self.mode}
	if self.pathGrants != nil {
		tellers = append(tellers, self.pathGrants)
	}
	return access.NewGroup(tellers...).Inject()
}

func (self *App) titleNote() string {
	if !self.agent.IsToolEnabled(title.Name) {
		return ""
	}

	hasAnswered := false
	for _, event := range self.events {
		if _, isTitled := agent.TitleFromEvent(event); isTitled {
			return ""
		}
		if event.Kind == agent.ModelMessageEvent {
			hasAnswered = true
		}
	}
	if !hasAnswered {
		return ""
	}

	return "This session has no title yet. Name the task with the " + title.Name + " tool."
}

func (self *App) interruptionNote() string {
	if !self.currentTurn.Cancelled() {
		return ""
	}

	note := "The previous turn was stopped before it finished"
	if reason := self.interruptionReason(); reason != "" {
		note += " because " + reason
	}

	return note + "."
}

func (self *App) interruptionReason() string {
	if reason := self.currentTurn.Reason(); reason != nil {
		return reason.Error()
	}

	return ""
}

func (self *App) takeTurn(turnEvent TurnEvent) {
	if !self.currentTurn.Observe(turnEvent) {
		return
	}

	if turnEvent.Update.Delta != nil {
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
	self.takeSessionTitle(event)

	if isSilentTurnNotice(event) && self.wasCutShort() && !self.wasPoked() {
		self.queuedTurn.MarkSilentTurn()
	}

	self.events = append(self.events, event)
	self.currentTurn.painter.DrawEvent(event)

	if self.currentTurn.painter.Stale() {
		self.redraw()
	}

	self.storeEvent(event)
}

func (self *App) storeEvent(event agent.Event) {
	if err := self.recorder.Event(event); err != nil {
		self.notifyFailure("The conversation could not be stored: " + err.Error())
	}
	self.showStorageWarnings()
}

func (self *App) notifyFailure(text string) {
	self.notify(agent.Event{Kind: agent.HarnessMessageEvent, Text: text, Status: agent.ErrorStatus})
}

func (self *App) notify(event agent.Event) {
	if event.Kind == agent.HarnessMessageEvent {
		self.showFeedback(systemFeedback, feedbackMessage{text: event.Text, status: event.Status})
		return
	}

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

func isSilentTurnNotice(event agent.Event) bool {
	return event.Kind == agent.HarnessMessageEvent && event.Text == agent.SilentTurnNotice
}

func (self *App) wasCutShort() bool {
	if len(self.events) == 0 {
		return false
	}

	return self.events[len(self.events)-1].Kind == agent.ModelReasoningEvent
}

func (self *App) wasPoked() bool {
	for _, event := range slices.Backward(self.events) {
		if event.Kind == agent.UserMessageEvent {
			return event.Text == turn.PokeMessage
		}
	}

	return false
}

func (self *App) finish() {
	self.currentTurn.MarkFinished(time.Now())
	self.screen.ReportProgress(false)

	var turnError error
	if self.currentTurn.Cancelled() {
		self.recordEvent(agent.Event{Kind: agent.InterruptionEvent, Text: self.interruptionReason()})
	} else if turnError = self.currentTurn.Error(); turnError != nil {
		self.recordEvent(agent.Event{Kind: agent.FailureEvent, Text: turnError.Error()})
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
	self.clearFeedback(commandFeedback)

	self.currentTurn.Finish()

	if turnError != nil && self.onFailure != nil {
		self.onFailure(turnError)
	}

	if self.isTransitionRequested() {
		return
	}

	if message, isQueued := self.currentTurn.TakeInterjections(); isQueued {
		self.queuedTurn.Replace(message)
	}

	queuedKind, message := self.queuedTurn.Take()
	switch queuedKind {
	case turn.Replacement:
		self.refreshPendingMessages()
		self.start(message)
	case turn.AccessChange:
		if message := self.accessMessage(); message != "" {
			self.start(message)
		}
	case turn.AccessNotice:
		self.refreshPendingMessages()
	case turn.Poke:
		self.refreshPendingMessages()
		self.start(message)
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
	warnings := self.recorder.TakeWarnings()
	if len(warnings) == 0 {
		return
	}

	messages := make([]string, len(warnings))
	for i, warning := range warnings {
		messages[i] = warning.Error()
	}
	self.notifyFailure(strings.Join(messages, "\n"))
}
