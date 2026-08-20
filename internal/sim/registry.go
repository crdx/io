package sim

import (
	"net/http"
	"strings"
)

type registryEntry struct {
	ID   string `json:"id"`
	Name string `json:"name"`

	ReasoningOptions []registryReasoning `json:"reasoning_options"`

	Limit struct {
		Context int `json:"context"`
		Output  int `json:"output"`
	} `json:"limit"`
}

type registryReasoning struct {
	Type   string   `json:"type"`
	Values []string `json:"values"`
}

type registryProvider struct {
	Models map[string]registryEntry `json:"models"`
}

const registryQuery = "providers"

func (self *Endpoint) serveRegistry(writer http.ResponseWriter, request *http.Request) {
	entry := registryEntry{
		ID:   self.scenario.Model,
		Name: self.scenario.Model,
		ReasoningOptions: []registryReasoning{
			{Type: "effort", Values: simulatedEfforts},
		},
	}

	entry.Limit.Context = simulatedContext
	entry.Limit.Output = simulatedOutput

	described := registryProvider{Models: map[string]registryEntry{self.scenario.Model: entry}}

	registry := map[string]registryProvider{}

	for name := range strings.SplitSeq(request.URL.Query().Get(registryQuery), ",") {
		if name != "" {
			registry[name] = described
		}
	}

	respond(writer, registry)
}
