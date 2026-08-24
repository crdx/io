package painter

import (
	"strings"

	"crdx.org/io/agent"
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

type Picasso struct {
	screen         *output.Screen
	toolBlock      *dynamic.Block
	rows           map[string]int
	answer         strings.Builder
	answerRenderer markdown.StreamRenderer
	reasoning      strings.Builder
	previousKind   agent.Kind

	isStale   bool
	isRunning bool

	getTool      func(string) (tool.Tool, bool)
	workspaceDir string
}

func New(screen *output.Screen, isRunning bool, getTool func(string) (tool.Tool, bool), workspaceDir string) *Picasso {
	return &Picasso{screen: screen, isRunning: isRunning, getTool: getTool, workspaceDir: workspaceDir}
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
		self.reasoning.WriteString(event.Text)
		if !self.screen.DrawReasoning(RenderReasoning(self.reasoning.String(), self.screen.Columns())) {
			self.isStale = true
		}
		self.screen.Seal()
		self.reasoning.Reset()

	case agent.ModelMessageEvent:
		self.discardProvisionalReasoning()
		self.answer.Reset()
		self.answer.WriteString(event.Text)
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
		if notice, said := caps.ModeNotice(event); said {
			self.Close(dynamic.Cancelled)
			self.screen.Line(NoticeStyle(agent.WarningStatus)(notice))
		}

	case agent.FailureEvent:
		self.Close(dynamic.Cancelled)
		self.screen.Line(style.Failure(event.Text))
	}
}

func (self *Picasso) ProvisionalDelta() agent.Delta {
	if self.reasoning.Len() > 0 {
		return agent.Delta{Kind: agent.ModelReasoningEvent, Text: self.reasoning.String()}
	}

	return agent.Delta{Kind: agent.ModelMessageEvent, Text: self.answer.String()}
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
	case agent.WarningStatus, "":
		return style.Stopped
	default:
		return style.Normal
	}
}

func getState(status agent.Status) dynamic.RowState {
	if status == agent.ErrorStatus {
		return dynamic.Failed
	}

	return dynamic.Done
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
	switch delta.Kind {
	case agent.ModelReasoningEvent:
		if self.reasoning.Len() == 0 && self.previousKind == agent.ModelReasoningEvent {
			self.screen.End()
		}
		self.reasoning.WriteString(delta.Text)
		if !self.screen.DrawReasoning(RenderReasoning(self.reasoning.String(), self.screen.Columns())) {
			self.isStale = true
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
		self.answer.WriteString(delta.Text)
		if !self.screen.DrawAnswer(self.answerRenderer.Render(self.answer.String(), self.screen.Columns())) {
			self.isStale = true
		}
	}
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
		event.Text,
		call.Measurements(event.Took, event.Stats),
	)

	if len(self.rows) == 0 {
		self.Close(dynamic.Done)
	}
}

func (self *Picasso) render(event agent.Event) string {
	if event.Kind == agent.StartupEvent {
		return startup.RenderEvent(event)
	}

	return NoticeStyle(event.Status)(event.Text)
}
