package painter

import (
	"path/filepath"
	"strings"

	"crdx.org/io/agent"
	"crdx.org/io/internal/util/pathutil"
	"crdx.org/io/tool"

	"crdx.org/io/cmd/oh/call"
	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/dynamic"
	"crdx.org/io/cmd/oh/markdown"
	"crdx.org/io/cmd/oh/output"
	"crdx.org/io/cmd/oh/skill"
	"crdx.org/io/cmd/oh/startup"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/oh/width"
)

const (
	readTool  = "read"
	shellTool = "bash"
)

// Painter renders conversation events and live deltas onto a terminal screen.
type Painter struct {
	Screen         *output.Screen
	toolBlock      *dynamic.Block          // the open block of tool calls
	rows           map[string]int          // which row of the block a call is being shown on
	answer         strings.Builder         // the answer so far, which is rendered again on every delta
	answerRenderer markdown.StreamRenderer // the live answer's rendering state
	reasoning      strings.Builder         // the reasoning so far, which is rendered again on every delta
	previousKind   agent.Kind              // the last completed event, which determines block separation

	isStale   bool // whether streamed prose outgrew what the screen can repair
	IsRunning bool // whether drawing is happening as events arrive

	GetTool      func(string) (tool.Tool, bool) // the tools a call may be rendered by
	WorkspaceDir string                         // the prefix omitted from paths inside the workspace
}

func (self *Painter) Describe(event agent.Event) agent.FallbackRendering {
	shown := event.FallbackRendering

	if calledTool, known := self.calledTool(event.Name); known {
		shown.ReadOnly = calledTool.ReadOnly()

		if parsedToolCall, err := calledTool.Parse(event.Arguments); err == nil {
			shown.Describe(parsedToolCall)
		} else {
			shown.Subject = tool.DescribeUnparsedArguments(calledTool, event.Arguments)
		}
	}

	shown.Subject = self.ShortenPathPrefix(shown.Subject)
	shown.Note = self.ShortenPathPrefix(shown.Note)
	shown.Emphasis.Source = self.ShortenPathPrefix(shown.Emphasis.Source)

	return shown
}

func (self *Painter) ShortenPathPrefix(value string) string {
	if self.WorkspaceDir != "" {
		for _, workspaceDir := range []string{self.WorkspaceDir, pathutil.Shorten(self.WorkspaceDir)} {
			rest, hasPrefix := strings.CutPrefix(value, workspaceDir)
			switch {
			case !hasPrefix:
				continue
			case rest == "":
				return ""
			case strings.HasPrefix(rest, string(filepath.Separator)):
				return strings.TrimPrefix(rest, string(filepath.Separator))
			case strings.HasPrefix(rest, " "):
				return strings.TrimPrefix(rest, " ")
			}
		}
	}

	return pathutil.Shorten(value)
}

func (self *Painter) DrawDelta(delta agent.Delta) {
	self.drawDeltaWithAnswerRendererReset(delta, true)
}

func (self *Painter) DrawRestoredDelta(delta agent.Delta, previous *Painter) {
	self.answerRenderer = previous.answerRenderer
	self.drawDeltaWithAnswerRendererReset(delta, false)
}

func (self *Painter) DrawEvent(event agent.Event) {
	switch {
	case event.Kind == agent.ModelReasoningEvent && self.previousKind == agent.ModelReasoningEvent && self.reasoning.Len() == 0:
		self.Screen.End()
	case event.Kind == agent.ModelMessageEvent && self.previousKind == agent.ModelMessageEvent && self.answer.Len() == 0:
		self.Screen.Blank()
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
		self.Screen.Blank()
		self.Screen.Line(RenderSubmittedMessage(event.Text, self.Screen.Columns()))
		self.Screen.End()
		self.Screen.Blank()

	case agent.ModelReasoningEvent:
		self.answer.Reset()
		self.reasoning.Reset()
		self.reasoning.WriteString(event.Text)
		if !self.Screen.DrawReasoning(RenderReasoning(self.reasoning.String(), self.Screen.Columns())) {
			self.isStale = true
		}
		self.Screen.Seal()
		self.reasoning.Reset()

	case agent.ModelMessageEvent:
		self.discardProvisionalReasoning()
		self.answer.Reset()
		self.answer.WriteString(event.Text)
		if !self.Screen.DrawAnswer(markdown.Render(self.answer.String(), self.Screen.Columns())) {
			self.isStale = true
		}
		self.Screen.Seal()
		self.answer.Reset()

	case agent.ToolCallRequestEvent:
		if self.toolBlock == nil {
			self.toolBlock = dynamic.NewBlock(self.Screen.Refresh)
			self.Screen.Open(self.toolBlock)
			self.rows = map[string]int{}
		}

		self.rows[event.ID] = self.toolBlock.Add(self.label(event, self.Describe(event)))

	case agent.ToolCallResultEvent:
		self.mark(event)

	case agent.StartupEvent, agent.HarnessMessageEvent:
		self.Screen.Line(self.render(event))

	case caps.ModeChange:
		if notice, said := caps.ModeNotice(event); said {
			self.Close(dynamic.Cancelled)
			self.Screen.Line(NoticeStyle(agent.WarningStatus)(notice))
		}

	case agent.FailureEvent:
		self.Close(dynamic.Cancelled)
		self.Screen.Line(style.Failure(event.Text))
	}
}

func (self *Painter) ProvisionalDelta() agent.Delta {
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

func (self *Painter) Stale() bool                   { return self.isStale }
func (self *Painter) HasOpenTools() bool            { return self.toolBlock != nil }
func (self *Painter) ToolRow(id string) (int, bool) { row, found := self.rows[id]; return row, found }

func (self *Painter) Close(state dynamic.RowState) {
	self.discardProvisionalReasoning()

	if self.toolBlock != nil {
		self.toolBlock.Close(state)
		self.toolBlock = nil
		self.rows = nil

		self.Screen.Seal()
	}
}

func (self *Painter) Stop() {
	if self.toolBlock != nil {
		self.toolBlock.Stop()
		self.toolBlock = nil
		self.rows = nil

		self.Screen.Seal()
	}
}

func (self *Painter) label(event agent.Event, shown agent.FallbackRendering) call.Label {
	label := call.Label{
		Name:      event.Name,
		Subject:   shown.Subject,
		Emphasis:  shown.Emphasis,
		Qualifier: shown.Note,
		ReadOnly:  shown.ReadOnly,
	}

	skillName, isSkillLoad := "", false
	if event.Name == readTool {
		skillName, isSkillLoad = skill.NameFromPath(shown.Subject)
	}

	switch {
	case event.Name == shellTool:
		label.Name = "$"
		label.NameStyle = style.Shell

	case isSkillLoad:
		label.Name = "load"
		label.NameStyle = style.Skill
		label.Accent = skillName
		label.AccentStyle = style.Skill
		label.Emphasis = tool.Emphasis{}
	}

	return label
}

func (self *Painter) calledTool(name string) (tool.Tool, bool) {
	if self.GetTool == nil {
		return nil, false
	}

	return self.GetTool(name)
}

func (self *Painter) drawDeltaWithAnswerRendererReset(delta agent.Delta, shouldResetAnswerRenderer bool) {
	switch delta.Kind {
	case agent.ModelReasoningEvent:
		if self.reasoning.Len() == 0 && self.previousKind == agent.ModelReasoningEvent {
			self.Screen.End()
		}
		self.reasoning.WriteString(delta.Text)
		if !self.Screen.DrawReasoning(RenderReasoning(self.reasoning.String(), self.Screen.Columns())) {
			self.isStale = true
		}

	case agent.ModelMessageEvent:
		self.discardProvisionalReasoning()
		if self.answer.Len() == 0 {
			if shouldResetAnswerRenderer {
				self.answerRenderer.Reset()
			}
			if self.previousKind == agent.ModelMessageEvent {
				self.Screen.Blank()
			}
		}
		self.answer.WriteString(delta.Text)
		if !self.Screen.DrawAnswer(self.answerRenderer.Render(self.answer.String(), self.Screen.Columns())) {
			self.isStale = true
		}
	}
}

func (self *Painter) discardProvisionalReasoning() {
	if self.reasoning.Len() == 0 {
		return
	}

	if !self.Screen.DiscardLive() {
		self.isStale = true
	}
	self.reasoning.Reset()
}

func (self *Painter) mark(event agent.Event) {
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

func (self *Painter) render(event agent.Event) string {
	if event.Kind == agent.StartupEvent {
		return startup.RenderEvent(event)
	}

	return NoticeStyle(event.Status)(event.Text)
}
