package chat

import (
	"context"
	"slices"
	"strings"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/internal/req"
)

const listTimeout = 30 * time.Second

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

	if err := self.listRequests().Get(ctx, address, self.headers(), &payload); err != nil {
		return nil, err
	}

	models := make([]agent.Model, 0, len(payload.Data))

	for _, listed := range payload.Data {
		if listed.ID != "" {
			models = append(models, agent.Model{ID: listed.ID, Efforts: slices.Clone(Efforts)})
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

func (self *Client) listRequests() *req.Client {
	client := req.New(listTimeout)
	if self.observer != nil {
		client.Observe(self.observer)
	}

	return client
}
