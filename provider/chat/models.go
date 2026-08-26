package chat

import (
	"context"
	"slices"
	"strings"

	"crdx.org/io/agent"
)

const completionsSuffix = "/chat/completions"

const modelsSuffix = "/models"

// SupportsCompletions reports whether a model ID names a model for the Chat Completions API.
func SupportsCompletions(id string) bool {
	switch {
	case strings.HasPrefix(id, "grok-"),
		strings.HasPrefix(id, "minimax-"),
		strings.HasPrefix(id, "qwen"),
		strings.HasSuffix(id, "-contributor"),
		strings.HasSuffix(id, "-luna"):
		return false
	default:
		return true
	}
}

// Models lists what the endpoint offers. The listing carries model names alone, so every one of
// them is offered the whole effort range and the endpoint decides what it honours.
//
// reference/chat-completions.md
func (self *Client) Models(ctx context.Context) ([]agent.Model, error) {
	address, listable := modelsAddress(self.URL)
	if !listable {
		return nil, nil
	}

	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}

	if err := self.observedRequests().Get(ctx, address, self.headers(), &payload); err != nil {
		return nil, err
	}

	models := make([]agent.Model, 0, len(payload.Data))

	for _, listed := range payload.Data {
		if listed.ID != "" {
			models = append(models, agent.Model{ID: listed.ID, EffortLevels: slices.Clone(Efforts)})
		}
	}

	return models, nil
}

func modelsAddress(turnAddress string) (string, bool) {
	prefix, found := strings.CutSuffix(turnAddress, completionsSuffix)
	if !found {
		return "", false
	}

	return prefix + modelsSuffix, true
}
