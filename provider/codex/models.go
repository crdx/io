package codex

import (
	"context"
	"slices"
	"strings"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/internal/req"
)

const listTimeout = 30 * time.Second

const responsesSuffix = "/codex/responses"

const modelsSuffix = "/models"

// Models lists what the endpoint offers. This listing is undocumented: it is what the ChatGPT web
// client asks for its own model picker, and a subscription token is not promised to be honoured
// for it. A caller that gets nothing back is expected to carry on with what it already knew.
func (self *Client) Models(ctx context.Context) ([]agent.Model, error) {
	address, listable := modelsAddress(self.URL)
	if !listable {
		return nil, nil
	}

	token, err := self.tokens.Token()
	if err != nil {
		return nil, err
	}

	var payload struct {
		Models []struct {
			Slug  string `json:"slug"`
			Title string `json:"title"`
		} `json:"models"`

		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}

	if err := self.listRequests().Get(ctx, address, self.headers(token), &payload); err != nil {
		return nil, err
	}

	models := make([]agent.Model, 0, len(payload.Models)+len(payload.Data))

	for _, listed := range payload.Models {
		if listed.Slug != "" {
			models = append(models, agent.Model{
				ID:      listed.Slug,
				Name:    listed.Title,
				Efforts: slices.Clone(Efforts),
			})
		}
	}

	for _, listed := range payload.Data {
		if listed.ID != "" {
			models = append(models, agent.Model{ID: listed.ID, Efforts: slices.Clone(Efforts)})
		}
	}

	return models, nil
}

func modelsAddress(turnAddress string) (string, bool) {
	prefix, found := strings.CutSuffix(turnAddress, responsesSuffix)
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
