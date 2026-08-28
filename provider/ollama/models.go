package ollama

import (
	"context"
	"slices"
	"sort"

	"crdx.org/io/agent"
)

const (
	modelsPath                 = "/api/tags"
	defaultContextWindow       = 32_768
	maximumMaxOutputTokens     = 32_768
	outputContextWindowDivisor = 4
)

var Efforts = []string{"none", "low", "medium", "high"}

type listedModel struct {
	Name         string   `json:"name"`
	Capabilities []string `json:"capabilities"`
	Details      struct {
		ContextWindowTokens int `json:"context_length"`
	} `json:"details"`
}

func (self *Client) Models(ctx context.Context) ([]agent.Model, error) {
	var payload struct {
		Models []listedModel `json:"models"`
	}

	if err := self.requests.Get(ctx, self.EndpointURL+modelsPath, nil, &payload); err != nil {
		return nil, err
	}

	models := make([]agent.Model, 0, len(payload.Models))
	for _, listed := range payload.Models {
		if listed.Name == "" || !slices.Contains(listed.Capabilities, "completion") ||
			!slices.Contains(listed.Capabilities, "tools") {
			continue
		}

		contextWindowTokens := listed.Details.ContextWindowTokens
		if contextWindowTokens <= 0 {
			contextWindowTokens = defaultContextWindow
		}

		effortLevels := []string{"none"}
		if slices.Contains(listed.Capabilities, "thinking") {
			effortLevels = slices.Clone(Efforts)
		}

		models = append(models, agent.Model{
			ID:                  listed.Name,
			EffortLevels:        effortLevels,
			ContextWindowTokens: contextWindowTokens,
			MaxOutputTokens:     maxOutputTokens(contextWindowTokens),
		})
	}

	sort.Slice(models, func(first int, second int) bool {
		return models[first].ID < models[second].ID
	})

	return models, nil
}

func maxOutputTokens(contextWindowTokens int) int {
	return max(1, min(contextWindowTokens/outputContextWindowDivisor, maximumMaxOutputTokens))
}
