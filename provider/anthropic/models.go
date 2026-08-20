package anthropic

import (
	"context"
	"strings"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/internal/req"
)

const (
	messagesSuffix = "/messages"
	modelsSuffix   = "/models"
	modelsPageSize = "1000"
)

const listTimeout = 30 * time.Second

type supported struct {
	Supported bool `json:"supported"`
}

type effortCapability struct {
	Supported bool      `json:"supported"`
	Low       supported `json:"low"`
	Medium    supported `json:"medium"`
	High      supported `json:"high"`
	XHigh     supported `json:"xhigh"`
	Max       supported `json:"max"`
}

func (self effortCapability) levels() []string {
	if !self.Supported {
		return nil
	}

	taken := map[string]bool{
		"low":    self.Low.Supported,
		"medium": self.Medium.Supported,
		"high":   self.High.Supported,
		"xhigh":  self.XHigh.Supported,
		"max":    self.Max.Supported,
	}

	var levels []string
	for _, level := range Efforts {
		if taken[level] {
			levels = append(levels, level)
		}
	}

	return levels
}

type listedModel struct {
	Type           string `json:"type"`
	ID             string `json:"id"`
	DisplayName    string `json:"display_name"`
	MaxInputTokens int    `json:"max_input_tokens"`
	MaxTokens      int    `json:"max_tokens"`

	Capabilities struct {
		Effort effortCapability `json:"effort"`
	} `json:"capabilities"`
}

// Models lists what the endpoint offers, newest first, which is the order it answers in. A model
// reporting no effort capability is recorded as taking none rather than as taking all of them:
// assuming the full range would offer a level the model may refuse, and a caller with a better
// informed source of its own is free to fill the gap in.
//
// reference/models.md
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
		Data []listedModel `json:"data"`
	}

	address += "?limit=" + modelsPageSize
	if err := self.listRequests().Get(ctx, address, self.headers(token), &payload); err != nil {
		return nil, err
	}

	models := make([]agent.Model, 0, len(payload.Data))

	for _, listed := range payload.Data {
		if listed.ID == "" {
			continue
		}

		models = append(models, agent.Model{
			ID:      listed.ID,
			Name:    listed.DisplayName,
			Efforts: listed.Capabilities.Effort.levels(),
			Context: listed.MaxInputTokens,
			Output:  listed.MaxTokens,
		})
	}

	return models, nil
}

func modelsAddress(turnAddress string) (string, bool) {
	prefix, found := strings.CutSuffix(turnAddress, messagesSuffix)
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
