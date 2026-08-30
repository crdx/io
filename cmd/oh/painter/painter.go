package painter

import (
	"strconv"
	"strings"

	"crdx.org/io/agent"
	"crdx.org/io/internal/util"
	"crdx.org/io/internal/util/strutil"
	"crdx.org/io/tool"

	"crdx.org/io/cmd/oh/call"
	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/dynamic"
	"crdx.org/io/cmd/oh/markdown"
	"crdx.org/io/cmd/oh/output"
	"crdx.org/io/cmd/oh/startup"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/oh/width"
)

const retryArgumentsCells = 120

type Picasso struct {
	screen         *output.Screen
	toolBlock      *dynamic.Block
	rows           map[string]int
	answer         liveText
	answerRenderer markdown.IncrementalRenderer
	reasoning      liveText
	previousKind   agent.Kind

	isStale       bool
	isRunning     bool
	streamingMode output.StreamingMode

	getTool      func(string) (tool.Tool, bool)
	workspaceDir string
}

func New(
	screen *output.Screen,
	isRunning bool,
	getTool func(string) (tool.Tool, bool),
	workspaceDir string,
	streamingMode output.StreamingMode,
) *Picasso {
	self := &Picasso{
		screen:        screen,
		isRunning:     isRunning,
		getTool:       getTool,
		workspaceDir:  workspaceDir,
		streamingMode: streamingMode,
	}

	self.answer.streamingMode = streamingMode
	self.reasoning.streamingMode = streamingMode

	return self
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
		self.screen.Line(RenderSubmittedMessage(event.Text, self.screen.Columns()))
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
		if !self.screen.DrawAnswer(markdown.Render(self.answer.String(), self.screen.Columns())) {
			self.isStale = true
		}
		self.screen.Seal()
		self.answer.Reset()

	case agent.ToolCallRequestEvent:
		if self.toolBlock == nil {
			self.toolBlock = dynamic.NewBlock(self.screen.Refresh)
			self.screen.Open(self.toolBlock)
			self.rows = map[string]int{}
		}

		self.rows[event.ID] = self.toolBlock.Add(call.LabelFor(event, self.getTool, self.workspaceDir))

	case agent.ToolCallResultEvent:
		self.mark(event)

	case agent.StartupEvent, agent.HarnessMessageEvent:
		self.screen.Line(self.render(event))

	case caps.ModeChange:
		if message, said := renderModeMessage(event); said {
			self.Close(dynamic.Cancelled)
			self.screen.Line(RenderSubmittedMessage(message, self.screen.Columns()))
		}

	case agent.RetryingEvent:
		self.Close(dynamic.Cancelled)
		self.screen.Line(style.Stopped(RenderRetry(event)))

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
	notice := "Request failed"
	if event.Attempt > 1 {
		notice += " on attempt " + strconv.Itoa(event.Attempt)
	}

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
	contentColumns := columns
	if contentColumns > 1 {
		contentColumns--
	}

	content := markdown.Render(text, contentColumns)
	for i, row := range content {
		content[i] = " " + row
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
		return style.Cancelled
	case agent.WarningStatus, "":
		return style.Stopped
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
	rendered := markdown.Render(thought, columns)
	plain := style.Plain(strings.Join(rendered, "\n"))
	stripped := strings.Join(strings.Fields(plain), " ")

	return width.Wrap(style.Reasoning(stripped), columns)
}

func (self *Picasso) Stale() bool { return self.isStale }

func (self *Picasso) Close(state dynamic.RowState) {
	self.discardProvisionalReasoning()
	self.settleAnswer()

	if self.toolBlock != nil {
		self.toolBlock.Close(state)
		self.toolBlock = nil
		self.rows = nil

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

		self.screen.Seal()
	}
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
	rows := RenderReasoning(self.reasoning.String(), self.screen.Columns())

	isTailHidden := !isSettled && self.streamingMode == output.StreamingModeLine
	if isTailHidden {
		rows = rows[:max(len(rows)-1, 0)]
	}

	self.reasoning.MarkDrawn(isTailHidden)

	if !self.screen.DrawReasoning(rows) {
		self.isStale = true
	}
}

func (self *Picasso) drawAnswer(isSettled bool) {
	rows := self.answerRenderer.Render(self.answer.String(), self.screen.Columns())

	isTailHidden := self.isTailHeldBack(isSettled)
	if isTailHidden {
		rows = rows[:max(len(rows)-1, 0)]
	}

	self.answer.MarkDrawn(isTailHidden)

	if !self.screen.DrawAnswer(rows) {
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

	index, known := self.rows[event.ID]
	if !known {
		return
	}

	delete(self.rows, event.ID)

	self.toolBlock.FinaliseRow(
		index,
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

	return NoticeStyle(event.Status)(strutil.PrintableLines(event.Text))
}

// RenderFailure presents the error a turn ended on, which is written down as the provider gave it
// and only made fit to read here, so that a session stored before this drew it the same way.
func RenderFailure(event agent.Event) string {
	return strutil.Capitalise(strutil.PrintableLines(event.Text))
}
