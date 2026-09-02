package painter

import (
	"strconv"
	"strings"

	"crdx.org/io/agent"
	"crdx.org/io/internal/toolresult"
	"crdx.org/io/internal/util"
	"crdx.org/io/internal/util/strutil"
	"crdx.org/io/tool"

	"crdx.org/io/cmd/oh/call"
	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/dynamic"
	"crdx.org/io/cmd/oh/link"
	"crdx.org/io/cmd/oh/markdown"
	"crdx.org/io/cmd/oh/output"
	"crdx.org/io/cmd/oh/pathgrant"
	"crdx.org/io/cmd/oh/startup"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/oh/width"
	"crdx.org/io/cmd/oh/work"
)

const retryArgumentsCells = 120

type Picasso struct {
	screen         *output.Screen
	toolBlock      *dynamic.Block
	rows           map[string]int
	labels         map[string]call.Label
	answer         liveText
	answerRenderer markdown.IncrementalRenderer
	reasoning      liveText
	previousKind   agent.Kind

	isStale               bool
	isRunning             bool
	streamingMode         output.StreamingMode
	resultLinkSessionName string

	getTool   func(string) (tool.Tool, bool)
	workspace *work.Space
}

func New(
	screen *output.Screen,
	isRunning bool,
	getTool func(string) (tool.Tool, bool),
	workspace *work.Space,
	streamingMode output.StreamingMode,
) *Picasso {
	self := &Picasso{
		screen:        screen,
		isRunning:     isRunning,
		getTool:       getTool,
		workspace:     workspace,
		streamingMode: streamingMode,
	}

	self.answer.streamingMode = streamingMode
	self.reasoning.streamingMode = streamingMode

	return self
}

func (self *Picasso) LinkToolResults(sessionName string) {
	self.resultLinkSessionName = sessionName
}

func (self *Picasso) DrawDelta(delta agent.Delta) {
	self.drawDeltaWithAnswerRendererReset(delta, true)
}

func (self *Picasso) DrawRestoredDelta(delta agent.Delta, previous *Picasso) {
	self.answerRenderer = previous.answerRenderer
	self.drawDeltaWithAnswerRendererReset(delta, false)
}

func (self *Picasso) DrawEvent(event agent.Event) {
	switch {
	case event.Kind == agent.ModelReasoningEvent && self.previousKind == agent.ModelReasoningEvent && self.reasoning.Len() == 0:
		self.screen.End()
	case event.Kind == agent.ModelMessageEvent && self.previousKind == agent.ModelMessageEvent && self.answer.Len() == 0:
		self.screen.Blank()
	}
	self.previousKind = event.Kind

	if event.Kind != agent.ModelReasoningEvent && event.Kind != agent.ModelMessageEvent {
		self.discardProvisionalReasoning()
		self.settleAnswer()
		self.answer.Reset()
	}

	switch event.Kind {
	case agent.UserMessageEvent:
		self.discardProvisionalReasoning()
		self.answer.Reset()
		self.Close(dynamic.Cancelled)
		self.screen.Blank()
		self.screen.Line(self.renderUserMessage(event.Text))
		self.screen.End()
		self.screen.Blank()

	case agent.ModelReasoningEvent:
		self.answer.Reset()
		self.reasoning.Reset()
		self.reasoning.Write(event.Text)
		self.drawReasoning(true)
		self.screen.Seal()
		self.reasoning.Reset()

	case agent.ModelMessageEvent:
		self.discardProvisionalReasoning()
		self.answer.Reset()
		self.answer.Write(event.Text)
		var renderedAnswer []string
		if self.screen.IsTerminal() {
			renderedAnswer = markdown.RenderWithHyperlinks(self.answer.Text(), self.screen.Columns())
		} else {
			renderedAnswer = markdown.Render(self.answer.Text(), self.screen.Columns())
		}
		if !self.screen.DrawAnswer(renderedAnswer) {
			self.isStale = true
		}
		self.screen.Seal()
		self.answer.Reset()

	case agent.ToolCallRequestEvent:
		if self.toolBlock == nil {
			self.toolBlock = dynamic.NewBlock(self.screen.Refresh)
			self.screen.Open(self.toolBlock)
			self.rows = map[string]int{}
			self.labels = map[string]call.Label{}
		}

		label := call.LabelFor(event, self.getTool, self.workspace)
		self.rows[event.ID] = self.toolBlock.Add(label)
		self.labels[event.ID] = label

	case agent.ToolCallResultEvent:
		self.mark(event)

	case agent.StartupEvent, agent.HarnessMessageEvent:
		self.screen.Line(self.render(event))

	case caps.ModeChange, pathgrant.Change:
		if message, isSaid := renderAccessMessage(event); isSaid {
			self.Close(dynamic.Cancelled)
			self.screen.Line(self.renderSubmittedMessage(message))
		}

	case agent.RetryingEvent:
		self.Close(dynamic.Cancelled)
		self.screen.Line(style.StoppedTurn(RenderRetry(event)))

	case agent.FailureEvent:
		self.Close(dynamic.Cancelled)
		self.screen.Line(style.Failure(RenderFailure(event)))

	case agent.StateChangeEvent, agent.InterruptionEvent:
	}
}

func (self *Picasso) ProvisionalDelta() agent.Delta {
	if self.reasoning.Len() > 0 {
		return agent.Delta{Kind: agent.ModelReasoningEvent, Text: self.reasoning.String()}
	}

	return agent.Delta{Kind: agent.ModelMessageEvent, Text: self.answer.String()}
}

func RenderRetry(event agent.Event) string {
	notice := "[#" + strconv.Itoa(event.Attempt) + "] Request failed"

	if event.Took > 0 {
		notice += "; retrying in " + util.CompactDuration(event.Took)
	} else {
		notice += "; retrying"
	}

	if event.Text != "" {
		notice += ": " + strutil.Capitalise(strutil.Flatten(strutil.FirstLine(event.Text)))
	}

	if event.Arguments != "" {
		notice += ": " + width.Elide(strutil.Flatten(event.Arguments), retryArgumentsCells)
	}

	return notice
}

func RenderSubmittedMessage(text string, columns int) string {
	return renderSubmittedMessage(text, columns, false, "", "")
}

func RenderSubmittedMessageWithHyperlinks(text string, columns int) string {
	return renderSubmittedMessage(text, columns, true, "", "")
}

func renderSubmittedMessage(text string, columns int, shouldRenderHyperlinks bool, workspace string, marker string) string {
	contentColumns := columns
	if contentColumns > 1 {
		contentColumns--
	}

	markerWidth := width.Of(marker)
	if markerWidth > 0 && contentColumns > markerWidth {
		contentColumns -= markerWidth
	}

	var content []string
	if shouldRenderHyperlinks {
		content = markdown.RenderWithHyperlinks(strutil.StripControl(text), contentColumns)
	} else {
		content = markdown.Render(strutil.StripControl(text), contentColumns)
	}
	for i, row := range content {
		if shouldRenderHyperlinks && workspace != "" {
			row = link.Render(row, workspace)
		}

		prefix := " "
		switch {
		case marker == "":
		case i == 0:
			prefix += marker
		default:
			prefix += strings.Repeat(" ", markerWidth)
		}
		content[i] = prefix + row
	}

	rows := append([]string{""}, content...)
	rows = append(rows, "")

	for i, row := range rows {
		if room := columns - style.Width(row); room > 0 {
			row += strings.Repeat(" ", room)
		}

		rows[i] = style.User(row)
	}

	return strings.Join(rows, "\n")
}

func NoticeStyle(severity agent.Status) style.Style {
	switch severity {
	case agent.InfoStatus:
		return style.Information
	case agent.SuccessStatus:
		return style.Success
	case agent.ErrorStatus:
		return style.Failure
	case agent.CancelledStatus:
		return style.CancelledCall
	case agent.WarningStatus, "":
		return style.StoppedTurn
	default:
		return style.Normal
	}
}

func getState(status agent.Status) dynamic.RowState {
	switch status {
	case agent.ErrorStatus:
		return dynamic.Failed
	case agent.CancelledStatus:
		return dynamic.Cancelled
	case agent.InfoStatus, agent.SuccessStatus, agent.WarningStatus:
		return dynamic.Done
	default:
		return dynamic.Done
	}
}

func RenderReasoning(thought string, columns int) []string {
	renderedRows := markdown.Render(thought, columns)
	plain := style.Plain(strings.Join(renderedRows, "\n"))
	strippedText := strings.Join(strings.Fields(plain), " ")

	return width.Wrap(style.Reasoning(strippedText), columns)
}

func (self *Picasso) Stale() bool { return self.isStale }

func (self *Picasso) Close(state dynamic.RowState) {
	self.discardProvisionalReasoning()
	self.settleAnswer()

	if self.toolBlock != nil {
		self.toolBlock.Close(state)
		self.toolBlock = nil
		self.rows = nil
		self.labels = nil

		self.screen.Seal()
	}
}

func (self *Picasso) End() {
	self.screen.End()
}

func (self *Picasso) Stop() {
	if self.toolBlock != nil {
		self.toolBlock.Stop()
		self.toolBlock = nil
		self.rows = nil
		self.labels = nil

		self.screen.Seal()
	}
}

func (self *Picasso) renderSubmittedMessage(text string) string {
	if self.screen.IsTerminal() {
		return RenderSubmittedMessageWithHyperlinks(text, self.screen.Columns())
	}

	return RenderSubmittedMessage(text, self.screen.Columns())
}

func (self *Picasso) renderUserMessage(text string) string {
	if !self.screen.IsTerminal() {
		return RenderSubmittedMessage(text, self.screen.Columns())
	}

	return renderSubmittedMessage(text, self.screen.Columns(), true, self.workspace.GetDir(), "")
}

func (self *Picasso) drawDeltaWithAnswerRendererReset(delta agent.Delta, shouldResetAnswerRenderer bool) {
	switch delta.Kind { //nolint:exhaustive // Only model prose event kinds can be deltas.
	case agent.ModelReasoningEvent:
		if self.reasoning.Len() == 0 && self.previousKind == agent.ModelReasoningEvent {
			self.screen.End()
		}
		self.reasoning.Write(delta.Text)
		if self.reasoning.IsDue() {
			self.drawReasoning(false)
		}

	case agent.ModelMessageEvent:
		self.discardProvisionalReasoning()
		if self.answer.Len() == 0 {
			if shouldResetAnswerRenderer {
				self.answerRenderer.Reset()
			}
			if self.previousKind == agent.ModelMessageEvent {
				self.screen.Blank()
			}
		}
		self.answer.Write(delta.Text)
		if self.answer.IsDue() {
			self.drawAnswer(false)
		}
	}
}

func (self *Picasso) settleAnswer() {
	if self.answer.IsOwed() {
		self.drawAnswer(true)
	}
}

func (self *Picasso) drawReasoning(isSettled bool) {
	rows := RenderReasoning(self.reasoning.Text(), self.screen.Columns())

	isTailHidden := !isSettled && self.streamingMode == output.StreamingModeLine

	if !self.screen.DrawReasoning(self.reasoning.Take(rows, isTailHidden)) {
		self.isStale = true
	}
}

func (self *Picasso) drawAnswer(isSettled bool) {
	var rows []string
	if self.screen.IsTerminal() {
		rows = self.answerRenderer.RenderWithHyperlinks(self.answer.Text(), self.screen.Columns())
	} else {
		rows = self.answerRenderer.Render(self.answer.Text(), self.screen.Columns())
	}

	if !self.screen.DrawAnswer(self.answer.Take(rows, self.isTailHeldBack(isSettled))) {
		self.isStale = true
	}
}

func (self *Picasso) isTailHeldBack(isSettled bool) bool {
	if isSettled || self.streamingMode != output.StreamingModeLine {
		return false
	}

	return !strings.HasSuffix(self.answer.String(), "\n") && !self.answerRenderer.IsTailMermaid()
}

func (self *Picasso) discardProvisionalReasoning() {
	if self.reasoning.Len() == 0 {
		return
	}

	if !self.screen.DiscardLive() {
		self.isStale = true
	}
	self.reasoning.Reset()
}

func (self *Picasso) mark(event agent.Event) {
	if self.toolBlock == nil {
		return
	}

	index, isKnown := self.rows[event.ID]
	if !isKnown {
		return
	}

	delete(self.rows, event.ID)
	label := self.labels[event.ID]
	delete(self.labels, event.ID)
	if self.resultLinkSessionName != "" {
		label.ResultURI = toolresult.URL(self.resultLinkSessionName, event.ID)
	}

	self.toolBlock.FinaliseRowWithLabel(
		index,
		label,
		getState(event.Status),
		event.Took,
		call.Summary(event),
		call.Measurements(event.Stats),
	)

	if len(self.rows) == 0 {
		self.Close(dynamic.Done)
	}
}

func (self *Picasso) render(event agent.Event) string {
	if event.Kind == agent.StartupEvent {
		return startup.RenderEvent(event, self.screen.Columns(), self.screen.IsTextSizingSupported())
	}

	return NoticeStyle(event.Status)(strutil.CapitaliseSentence(strutil.PrintableLines(event.Text)))
}

func RenderFailure(event agent.Event) string {
	return strutil.CapitaliseSentence(strutil.PrintableLines(event.Text))
}
