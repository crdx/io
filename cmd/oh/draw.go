package main

import (
	"strings"

	"crdx.org/io/agent"
	"crdx.org/io/tool"

	"crdx.org/io/cmd/oh/markdown"
	"crdx.org/io/cmd/oh/output"
	"crdx.org/io/cmd/oh/skill"
	"crdx.org/io/cmd/oh/status"
	"crdx.org/io/cmd/oh/theme"
)

const readTool = "read"

type painter struct {
	screen *output.Output  // where the conversation is drawn
	live   bool            // whether drawing is happening as events arrive
	block  *status.Block   // the open block of calls
	rows   map[string]int  // which row of the block a call is being shown on
	answer strings.Builder // the answer so far, which is rendered again on every delta
	stale  bool            // whether the answer outgrew what the screen can repair

	tools func(string) (tool.Tool, bool) // the tools a call may be rendered by
	shell string                         // what the shell tool was named, so a call to it is drawn as a prompt
}

func (self *painter) describe(event agent.Event) (string, string, string, string) {
	if self.tools != nil {
		if calledTool, known := self.tools(event.Name); known {
			if parsedCall, err := calledTool.Parse(event.Arguments); err == nil {
				focus := ""
				if focusedCall, ok := parsedCall.(tool.FocusedCall); ok {
					focus = focusedCall.Focus()
				}

				syntax := ""
				if syntaxCall, ok := parsedCall.(tool.SyntaxCall); ok {
					syntax = syntaxCall.Syntax()
				}

				return parsedCall.Render(), parsedCall.Detail(), focus, syntax
			}
		}
	}

	return event.Render, event.Detail, event.Focus, event.Syntax
}

func (self *painter) draw(event agent.Event) {
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
		self.screen.Line(theme.Reasoning(flatten(event.Text)))

	case agent.Text:
		self.answer.WriteString(event.Text)

		if !self.screen.Draw(markdown.Render(self.answer.String(), self.screen.Columns())) {
			self.stale = true
		}

	case agent.Call:
		if self.block == nil {
			self.block = self.screen.Status()
			self.rows = map[string]int{}
		}

		renderedArgs, detail, focus, syntax := self.describe(event)

		// TODO(x): rewrite this mess
		name := event.Name
		var nameStyle theme.Style
		accent := ""
		var accentStyle theme.Style
		if event.Name == readTool {
			if skillName, isSkill := skill.NameFromPath(renderedArgs); isSkill {
				name = "load"
				nameStyle = theme.Skill
				accent = skillName
				accentStyle = theme.Skill
				focus = ""
			}
		}
		if event.Name == self.shell {
			name = "$"
			nameStyle = theme.Shell
		}

		self.rows[event.ID] = self.block.Add(status.Label{
			Name:        name,
			NameStyle:   nameStyle,
			Args:        renderedArgs,
			Focus:       focus,
			Syntax:      syntax,
			Detail:      detail,
			ReadOnly:    event.ReadOnly,
			Accent:      accent,
			AccentStyle: accentStyle,
		})

	case agent.Result:
		self.mark(event)
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

	self.block.MarkWithStats(index, outcome(event.Failed), event.Took, event.Text, event.Statistics)

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
		if room := columns - theme.Width(row); room > 0 {
			row += strings.Repeat(" ", room)
		}

		rows[index] = theme.User(row)
	}

	return strings.Join(rows, "\n")
}

func outcome(failed bool) status.State {
	if failed {
		return status.Failed
	}

	return status.Done
}

func flatten(thought string) string {
	var out strings.Builder

	for index, line := range strings.Split(thought, "\n") {
		line = strings.TrimSpace(strings.ReplaceAll(line, "**", ""))
		if line == "" {
			continue
		}

		if index > 0 && out.Len() > 0 {
			out.WriteString(" · ")
		}

		out.WriteString(line)
	}

	return out.String()
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
