package codex

import (
	"encoding/json"

	"crdx.org/io/tool"
)

type functionTool struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Strict      bool   `json:"strict"`

	Schema tool.Schema `json:"parameters"` // never omitempty: a tool taking nothing still needs one
}

func describe(tools []tool.Definition) []functionTool {
	offeredTools := make([]functionTool, len(tools))

	for index, offer := range tools {
		offeredTools[index] = functionTool{
			Type:        "function",
			Name:        offer.Name,
			Description: offer.Description,
			Strict:      false,
			Schema:      offer.Schema,
		}
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
