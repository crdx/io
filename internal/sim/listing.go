package sim

import (
	"encoding/json"
	"net/http"
)

const (
	simulatedContext = 200_000
	simulatedOutput  = 64_000
)

var simulatedEfforts = []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}

type listing struct {
	Data []listedModel `json:"data"`
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

type effortCapability struct {
	Supported bool           `json:"supported"`
	Low       levelSupported `json:"low"`
	Medium    levelSupported `json:"medium"`
	High      levelSupported `json:"high"`
	XHigh     levelSupported `json:"xhigh"`
	Max       levelSupported `json:"max"`
}

type levelSupported struct {
	Supported bool `json:"supported"`
}

func (self *Endpoint) serveListing(writer http.ResponseWriter) {
	offered := listedModel{
		Type:           "model",
		ID:             self.scenario.Model,
		DisplayName:    self.scenario.Model,
		MaxInputTokens: simulatedContext,
		MaxTokens:      simulatedOutput,
	}

	offered.Capabilities.Effort = effortCapability{
		Supported: true,
		Low:       levelSupported{Supported: true},
		Medium:    levelSupported{Supported: true},
		High:      levelSupported{Supported: true},
		XHigh:     levelSupported{Supported: true},
		Max:       levelSupported{Supported: true},
	}

	respond(writer, listing{Data: []listedModel{offered}})
}

type ollamaListing struct {
	Models []ollamaListedModel `json:"models"`
}

type ollamaListedModel struct {
	Name         string   `json:"name"`
	Capabilities []string `json:"capabilities"`
	Details      struct {
		ContextWindowTokens int `json:"context_length"`
	} `json:"details"`
}

func (self *Endpoint) serveOllamaListing(writer http.ResponseWriter) {
	offered := ollamaListedModel{
		Name:         self.scenario.Model,
		Capabilities: []string{"completion", "thinking", "tools"},
	}
	offered.Details.ContextWindowTokens = simulatedContext

	respond(writer, ollamaListing{Models: []ollamaListedModel{offered}})
}

func respond(writer http.ResponseWriter, document any) {
	writer.Header().Set("Content-Type", "application/json")

	_ = json.NewEncoder(writer).Encode(document) //nolint:errchkjson // the documents encode safely
}
