package main

import (
	"path/filepath"
	"strings"

	"crdx.org/io/agent"
	"crdx.org/io/internal/pathutil"
	"crdx.org/io/tool"

	"crdx.org/io/cmd/oh/call"
	"crdx.org/io/cmd/oh/dynamic"
	"crdx.org/io/cmd/oh/markdown"
	"crdx.org/io/cmd/oh/output"
	"crdx.org/io/cmd/oh/skill"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/oh/width"
)

const (
	readTool  = "read"
	shellTool = "bash"
)

type Painter struct {
	screen    *output.Screen
	toolBlock *dynamic.Block  // the open block of tool calls
	rows      map[string]int  // which row of the block a call is being shown on
	answer    strings.Builder // the answer so far, which is rendered again on every delta
	reasoning strings.Builder // the reasoning so far, which is rendered again on every delta

	isStale   bool // whether streamed prose outgrew what the screen can repair
	isRunning bool // whether drawing is happening as events arrive

	getTool      func(string) (tool.Tool, bool) // the tools a call may be rendered by
	workspaceDir string                         // the prefix omitted from paths inside the workspace
}

func (self *Painter) describe(event agent.Event) agent.Rendering {
	shown := event.Rendering

	if calledTool, known := self.calledTool(event.Name); known {
		shown.ReadOnly = calledTool.ReadOnly()

		if parsedToolCall, err := calledTool.Parse(event.Arguments); err == nil {
			shown.Describe(parsedToolCall)
		} else {
			shown.Subject = tool.DescribeUnparsedArguments(calledTool, event.Arguments)
		}
	}

	shown.Subject = self.shortenPathPrefix(shown.Subject)
	shown.Note = self.shortenPathPrefix(shown.Note)

	return shown
}

func (self *Painter) label(event agent.Event, shown agent.Rendering) call.Label {
	label := call.Label{
		Name:      event.Name,
		Subject:   shown.Subject,
		Highlight: shown.Highlight,
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
		label.Highlight = tool.Highlight{}
	}

	return label
}

func (self *Painter) shortenPathPrefix(value string) string {
	if self.workspaceDir != "" {
		for _, workspaceDir := range []string{self.workspaceDir, pathutil.Shorten(self.workspaceDir)} {
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

func (self *Painter) calledTool(name string) (tool.Tool, bool) {
	if self.getTool == nil {
		return nil, false
	}

	return self.getTool(name)
}

func (self *Painter) drawEvent(event agent.Event) {
	if event.Kind != agent.ModelReasoning && self.reasoning.Len() > 0 {
		self.screen.End()
		self.reasoning.Reset()
		if event.Kind == agent.ModelMessage {
			self.screen.Blank()
		}
	}
	if event.Kind == agent.ModelReasoning && self.answer.Len() > 0 {
		self.screen.End()
	}
	if event.Kind != agent.ModelMessage {
		self.answer.Reset()
	}

	switch event.Kind {
	case agent.UserMessage:
		self.close(dynamic.Cancelled)
		self.screen.Blank()
		self.screen.Line(renderSubmittedMessage(event.Text, self.screen.Columns()))
		self.screen.End()
		self.screen.Blank()

	case agent.ModelReasoning:
		self.reasoning.WriteString(event.Text)
		rows := renderReasoning(self.reasoning.String(), self.screen.Columns())
		if !self.screen.DrawReasoning(rows) {
			self.isStale = true
		}

	case agent.ModelMessage:
		self.answer.WriteString(event.Text)

		if !self.screen.DrawAnswer(markdown.Render(self.answer.String(), self.screen.Columns())) {
			self.isStale = true
		}

	case agent.ToolCallRequest:
		if self.toolBlock == nil {
			self.toolBlock = dynamic.NewBlock(self.screen.Refresh)
			self.screen.Open(self.toolBlock)
			self.rows = map[string]int{}
		}

		self.rows[event.ID] = self.toolBlock.Add(self.label(event, self.describe(event)))

	case agent.ToolCallResult:
		self.mark(event)

	case agent.Startup, agent.HarnessMessage:
		self.screen.Line(self.render(event))

	case agent.Failure:
		self.close(dynamic.Cancelled)
		self.screen.Line(style.Failure(event.Text))
	}
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
		getState(event.Failed),
		event.Took,
		event.Text,
		call.Measurements(event.Took, event.Stats),
	)

	if len(self.rows) == 0 {
		self.close(dynamic.Done)
	}
}

func renderSubmittedMessage(text string, columns int) string {
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

func (self *Painter) render(event agent.Event) string {
	if event.Kind == agent.Startup {
		return renderStartupEvent(event)
	}

	return noticeStyle(event.Failed)(event.Text)
}

func noticeStyle(failed bool) style.Style {
	if failed {
		return style.Failure
	}

	return style.Stopped
}

func getState(failed bool) dynamic.RowState {
	if failed {
		return dynamic.Failed
	}

	return dynamic.Done
}

func renderReasoning(thought string, columns int) []string {
	rendered := markdown.Render(thought, columns)
	plain := style.Plain(strings.Join(rendered, "\n"))
	stripped := strings.Join(strings.Fields(plain), " ")

	return width.Wrap(style.Reasoning(stripped), columns)
}

func (self *Painter) close(state dynamic.RowState) {
	if self.toolBlock != nil {
		self.toolBlock.Close(state)
		self.toolBlock = nil
		self.rows = nil

		self.screen.Seal()
	}
}

func (self *Painter) stop() {
	if self.toolBlock != nil {
		self.toolBlock.Stop()
		self.toolBlock = nil
		self.rows = nil

		self.screen.Seal()
	}
}
