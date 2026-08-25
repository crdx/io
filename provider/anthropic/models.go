package anthropic

import (
	"context"
	"strconv"
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

var adaptiveThinkingSince = generation{major: 4, minor: 6}

func SupportsAdaptiveThinking(id string) bool {
	found, isVersioned := generationOf(id)

	return !isVersioned || !found.precedes(adaptiveThinkingSince)
}

type generation struct {
	major int
	minor int
}

func (self generation) precedes(other generation) bool {
	if self.major != other.major {
		return self.major < other.major
	}

	return self.minor < other.minor
}

const datedSnapshotLength = len("20060102")

func generationOf(id string) (generation, bool) {
	var numbers []int

	for segment := range strings.SplitSeq(id, "-") {
		if len(segment) == datedSnapshotLength {
			continue
		}

		number, err := strconv.Atoi(segment)
		if err != nil {
			continue
		}

		if numbers = append(numbers, number); len(numbers) == 2 {
			break
		}
	}

	switch len(numbers) {
	case 0:
		return generation{}, false
	case 1:
		return generation{major: numbers[0]}, true
	default:
		return generation{major: numbers[0], minor: numbers[1]}, true
	}
}

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
			ID:                  listed.ID,
			Name:                listed.DisplayName,
			EffortLevels:        listed.Capabilities.Effort.levels(),
			ContextWindowTokens: listed.MaxInputTokens,
			MaxOutputTokens:     listed.MaxTokens,
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
