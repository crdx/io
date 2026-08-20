package anthropic

import (
	"encoding/json"
	"strings"

	"crdx.org/io/tool"
)

type functionTool struct {
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Cache       *cacheControl `json:"cache_control,omitempty"`

	Schema tool.Schema `json:"input_schema"` // never omitempty: a tool taking nothing still needs one
}

type cacheControl struct {
	Type string `json:"type"`
}

func ephemeral() *cacheControl {
	return &cacheControl{Type: "ephemeral"}
}

var claudeCodeNames = map[string]string{
	"read":            "Read",
	"write":           "Write",
	"edit":            "Edit",
	"bash":            "Bash",
	"grep":            "Grep",
	"glob":            "Glob",
	"askuserquestion": "AskUserQuestion",
	"enterplanmode":   "EnterPlanMode",
	"exitplanmode":    "ExitPlanMode",
	"killshell":       "KillShell",
	"notebookedit":    "NotebookEdit",
	"skill":           "Skill",
	"task":            "Task",
	"taskoutput":      "TaskOutput",
	"todowrite":       "TodoWrite",
	"webfetch":        "WebFetch",
	"websearch":       "WebSearch",
}

func toClaudeCodeName(name string) string {
	if canonical, found := claudeCodeNames[strings.ToLower(name)]; found {
		return canonical
	}

	return name
}

func fromClaudeCodeName(name string, known []string) string {
	for _, candidate := range known {
		if strings.EqualFold(candidate, name) {
			return candidate
		}
	}

	return name
}

func describe(tools []tool.Definition) []functionTool {
	offeredTools := make([]functionTool, len(tools))

	for index, offer := range tools {
		offeredTools[index] = functionTool{
			Name:        toClaudeCodeName(offer.Name),
			Description: offer.Description,
			Schema:      offer.Schema,
		}
	}

	if len(offeredTools) > 0 {
		offeredTools[len(offeredTools)-1].Cache = ephemeral()
	}

	return offeredTools
}

// ToolsSize is the number of bytes the tools occupy in their provider wire representation.
func ToolsSize(tools []tool.Tool) int {
	definitions := make([]tool.Definition, len(tools))
	for index, offeredTool := range tools {
		definitions[index] = tool.Describe(offeredTool)
	}

	encodedTools, _ := json.Marshal(describe(definitions)) //nolint:errchkjson // all fields have safe encoders
	return len(encodedTools)
}
