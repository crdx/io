package chatcompletions

import (
	"context"
	"slices"
	"strings"

	"crdx.org/io/agent"
)

const completionsSuffix = "/chat/completions"

const modelsSuffix = "/models"

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

	if err := self.observedRequests().Get(ctx, address, self.headers("application/json"), &payload); err != nil {
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
