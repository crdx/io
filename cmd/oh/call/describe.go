package call

import (
	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/skill"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/oh/work"
	"crdx.org/io/internal/util/strutil"
	"crdx.org/io/tool"
)

const (
	readTool      = "read"
	shellTool     = "bash"
	webSearchTool = "web_search"
	webFetchTool  = "web_fetch"
)

type ToolLookup func(string) (tool.Tool, bool)

func Summary(event agent.Event) string {
	if event.Status != agent.SuccessStatus || event.Name == shellTool {
		return event.Text
	}

	return ""
}

func Describe(event agent.Event, getTool ToolLookup, workspace *work.Space) agent.FallbackRendering {
	rendering := event.FallbackRendering
	if getTool != nil {
		if calledTool, isKnown := getTool(event.Name); isKnown {
			rendering.ReadOnly = calledTool.ReadOnly()
			if parsedToolCall, err := calledTool.Parse(event.Arguments); err == nil {
				rendering.Describe(parsedToolCall)
			} else {
				rendering.Subject = tool.DescribeUnparsedArguments(calledTool, event.Arguments)
			}
		}
	}
	return plain(shortenPaths(rendering, workspace))
}

func plain(rendering agent.FallbackRendering) agent.FallbackRendering {
	rendering.Subject = strutil.Printable(rendering.Subject)
	rendering.Note = strutil.Printable(rendering.Note)

	return rendering
}

func LabelFor(event agent.Event, getTool ToolLookup, workspace *work.Space) Label {
	rendering := Describe(event, getTool, workspace)
	label := Label{
		Name:      event.Name,
		Subject:   rendering.Subject,
		Emphasis:  rendering.Emphasis,
		Qualifier: rendering.Note,
		ReadOnly:  rendering.ReadOnly,
	}

	skillName, isSkillLoad := "", false
	if event.Name == readTool {
		skillName, isSkillLoad = skill.NameFromPath(rendering.Subject)
	}

	if toolLabel, isKnown := toolLabels[event.Name]; isKnown {
		label.Name = toolLabel.name
		label.NameStyle = toolLabel.style
	}

	if isSkillLoad {
		label.Name = "load"
		label.NameStyle = style.Skill
		label.Accent = skillName
		label.AccentStyle = style.Skill
		label.Emphasis = tool.Emphasis{}
	}
	return label
}

var toolLabels = map[string]struct {
	name  string
	style style.Style
}{
	shellTool:     {"$", style.Shell},
	webSearchTool: {"search", style.Network},
	webFetchTool:  {"fetch", style.Network},
}
