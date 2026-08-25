package call

import (
	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/skill"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/tool"
)

const (
	readTool      = "read"
	shellTool     = "bash"
	webSearchTool = "web_search"
	webFetchTool  = "web_fetch"
)

type ToolLookup func(string) (tool.Tool, bool)

func Describe(event agent.Event, getTool ToolLookup, workspaceDir string) agent.FallbackRendering {
	shown := event.FallbackRendering
	if getTool != nil {
		if calledTool, known := getTool(event.Name); known {
			shown.ReadOnly = calledTool.ReadOnly()
			if parsedToolCall, err := calledTool.Parse(event.Arguments); err == nil {
				shown.Describe(parsedToolCall)
			} else {
				shown.Subject = tool.DescribeUnparsedArguments(calledTool, event.Arguments)
			}
		}
	}
	return shortenPaths(shown, workspaceDir)
}

func LabelFor(event agent.Event, getTool ToolLookup, workspaceDir string) Label {
	shown := Describe(event, getTool, workspaceDir)
	label := Label{
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

	if toolLabel, known := toolLabels[event.Name]; known {
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
