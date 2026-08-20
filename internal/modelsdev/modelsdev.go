// Package modelsdev reads the models.dev registry, which describes models across many providers.
package modelsdev

import (
	"context"
	"slices"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/internal/req"
)

// Endpoint is the whole registry, served as one document.
//
// https://models.dev
const Endpoint = "https://models.dev/api.json"

const fetchTimeout = 60 * time.Second

// Registry is what the registry knows, by provider name and then by model name. The names are
// models.dev's own, which is why Provider takes the one to look under rather than assuming ours.
type Registry map[string]map[string]agent.Model

type entry struct {
	ID   string `json:"id"`
	Name string `json:"name"`

	ReasoningOptions []struct {
		Type   string   `json:"type"`
		Values []string `json:"values"`
	} `json:"reasoning_options"`

	Limit struct {
		Context int `json:"context"`
		Output  int `json:"output"`
	} `json:"limit"`
}

func (self entry) efforts() []string {
	for _, option := range self.ReasoningOptions {
		if option.Type == "effort" && len(option.Values) > 0 {
			return slices.Clone(option.Values)
		}
	}

	return nil
}

func (self entry) model(name string) agent.Model {
	identifier := self.ID
	if identifier == "" {
		identifier = name
	}

	return agent.Model{
		ID:      identifier,
		Name:    self.Name,
		Efforts: self.efforts(),
		Context: self.Limit.Context,
		Output:  self.Limit.Output,
	}
}

// Fetch reads the registry from address, or from the real one when that is empty. It is several
// megabytes describing every provider it knows, so it is read once and looked up per provider
// rather than fetched per provider.
func Fetch(ctx context.Context, address string, observer req.Observer) (Registry, error) {
	requests := req.New(fetchTimeout)
	if observer != nil {
		requests.Observe(observer)
	}

	if address == "" {
		address = Endpoint
	}

	var payload map[string]struct {
		Models map[string]entry `json:"models"`
	}

	if err := requests.Get(ctx, address, nil, &payload); err != nil {
		return nil, err
	}

	registry := make(Registry, len(payload))

	for providerName, described := range payload {
		models := make(map[string]agent.Model, len(described.Models))
		for modelName, described := range described.Models {
			models[modelName] = described.model(modelName)
		}

		registry[providerName] = models
	}

	return registry, nil
}

// Provider is what the registry holds for one provider, and nothing when it holds none.
func (self Registry) Provider(name string) map[string]agent.Model {
	return self[name]
}
