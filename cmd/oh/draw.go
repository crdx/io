package main

import (
	"path/filepath"
	"strings"

	"crdx.org/io/agent"
	"crdx.org/io/internal/pathutil"
	"crdx.org/io/tool"

	"crdx.org/io/cmd/oh/markdown"
	"crdx.org/io/cmd/oh/output"
	"crdx.org/io/cmd/oh/skill"
	"crdx.org/io/cmd/oh/status"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/oh/width"
)

const readTool = "read"

type painter struct {
	screen    *output.Output
	live      bool            // whether drawing is happening as events arrive
	block     *status.Block   // the open block of calls
	rows      map[string]int  // which row of the block a call is being shown on
	answer    strings.Builder // the answer so far, which is rendered again on every delta
	reasoning strings.Builder // the reasoning so far, which is rendered again on every delta
	stale     bool            // whether streamed prose outgrew what the screen can repair

	tools        func(string) (tool.Tool, bool) // the tools a call may be rendered by
	shell        string                         // what the shell tool was named, so a call to it is drawn as a prompt
	workspaceDir string                         // the prefix omitted from paths inside the workspace
}

func (self *painter) describe(event agent.Event) (string, string, tool.Highlight) {
	subject := event.Subject
	qualifier := event.Qualifier
	highlight := event.Highlight

	calledTool, known := self.calledTool(event.Name)
	if known {
		parsedCall, err := calledTool.Parse(event.Arguments)
		if err != nil {
			subject = tool.DescribeUnparsedArguments(calledTool, event.Arguments)
		} else {
			subject = parsedCall.Subject()
			qualifier = parsedCall.Qualifier()
			highlight = parsedCall.Highlight()
		}
	}

	return self.shortenPathPrefix(subject), self.shortenPathPrefix(qualifier), highlight
}

func (self *painter) shortenPathPrefix(value string) string {
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

func (self *painter) calledTool(name string) (tool.Tool, bool) {
	if self.tools == nil {
		return nil, false
	}

	return self.tools(name)
}

func (self *painter) draw(event agent.Event) {
	if event.Kind != agent.Reasoning && self.reasoning.Len() > 0 {
		self.screen.End()
		self.reasoning.Reset()
		if event.Kind == agent.Text {
			self.screen.Blank()
		}
	}
	if event.Kind == agent.Reasoning && self.answer.Len() > 0 {
		self.screen.End()
	}
	if event.Kind != agent.Text {
		self.answer.Reset()
	}

	switch event.Kind {
	case agent.Prompt:
		self.close(status.Cancelled)
		self.screen.Blank()
		self.screen.Line(renderSubmittedMessage(event.Text, self.screen.Columns()))
		self.screen.End()
		self.screen.Blank()

	case agent.Reasoning:
		self.reasoning.WriteString(event.Text)
		rows := renderReasoning(self.reasoning.String(), self.screen.Columns())
		if !self.screen.DrawReasoning(rows) {
			self.stale = true
		}

	case agent.Text:
		self.answer.WriteString(event.Text)

		if !self.screen.DrawAnswer(markdown.Render(self.answer.String(), self.screen.Columns())) {
			self.stale = true
		}

	case agent.Call:
		if self.block == nil {
			self.block = self.screen.Status()
			self.rows = map[string]int{}
		}

		subject, qualifier, highlight := self.describe(event)

		// TODO(x): rewrite this mess
		name := event.Name
		var nameStyle style.Style
		accent := ""
		var accentStyle style.Style
		if event.Name == readTool {
			if skillName, isSkill := skill.NameFromPath(subject); isSkill {
				name = "load"
				nameStyle = style.Skill
				accent = skillName
				accentStyle = style.Skill
				highlight = tool.Highlight{}
			}
		}
		if event.Name == self.shell {
			name = "$"
			nameStyle = style.Shell
		}

		self.rows[event.ID] = self.block.Add(status.Label{
			Name:        name,
			NameStyle:   nameStyle,
			Subject:     subject,
			Highlight:   highlight,
			Qualifier:   qualifier,
			ReadOnly:    event.ReadOnly,
			Accent:      accent,
			AccentStyle: accentStyle,
		})

	case agent.Result:
		self.mark(event)

	case agent.Failure:
		self.close(status.Cancelled)
		self.screen.Line(style.Failure(event.Text))
	}
}

func (self *painter) mark(event agent.Event) {
	if self.block == nil {
		return
	}

	index, known := self.rows[event.ID]
	if !known {
		return
	}

	delete(self.rows, event.ID)

	self.block.MarkWithStats(index, outcome(event.Failed), event.Took, event.Text, event.Stats)

	if len(self.rows) == 0 {
		self.close(status.Done)
	}
}

func renderSubmittedMessage(text string, columns int) string {
	contentColumns := columns
	if contentColumns > 1 {
		contentColumns-- // the left padding belongs to the message too
	}

	content := markdown.Render(text, contentColumns)
	for index, row := range content {
		content[index] = " " + row
	}

	rows := append([]string{""}, content...)
	rows = append(rows, "")

	for index, row := range rows {
		if room := columns - style.Width(row); room > 0 {
			row += strings.Repeat(" ", room)
		}

		rows[index] = style.User(row)
	}

	return strings.Join(rows, "\n")
}

func outcome(failed bool) status.State {
	if failed {
		return status.Failed
	}

	return status.Done
}

func renderReasoning(thought string, columns int) []string {
	rendered := markdown.Render(thought, columns)
	plain := style.Plain(strings.Join(rendered, "\n"))
	stripped := strings.Join(strings.Fields(plain), " ")

	return width.Wrap(style.Reasoning(stripped), columns)
}

func (self *painter) close(state status.State) {
	if self.block != nil {
		self.block.Close(state)
		self.block = nil
		self.rows = nil
	}
}

func (self *painter) stop() {
	if self.block != nil {
		self.block.Stop()
		self.block = nil
		self.rows = nil
	}
}
