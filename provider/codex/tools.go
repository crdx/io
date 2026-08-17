package codex

import "crdx.org/io/tool"

type functionTool struct {
	Type        string `json:"type"`        // the kind of tool
	Name        string `json:"name"`        // what the tool is called
	Description string `json:"description"` // what the tool does
	Strict      bool   `json:"strict"`      // whether its schema is enforced strictly

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
