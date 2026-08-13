package codex

import "crdx.org/io/tool"

type functionTool struct {
	Type        string      `json:"type"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Strict      bool        `json:"strict"`
	Schema      tool.Schema `json:"parameters,omitempty"`
}

func describe(tools []tool.Definition) []functionTool {
	offered := make([]functionTool, len(tools))

	for index, offer := range tools {
		offered[index] = functionTool{
			Type:        "function",
			Name:        offer.Name,
			Description: offer.Description,
			Strict:      false,
			Schema:      offer.Schema,
		}
	}

	return offered
}
