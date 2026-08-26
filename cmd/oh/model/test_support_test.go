package model

import (
	"context"
	"io"
	"os"
	"path/filepath"

	"crdx.org/io/agent"
)

const (
	endpointVariable   = "OH_ENDPOINT_URL"
	codexProvider      = CodexProvider
	opencodeGoProvider = OpencodeGoProvider
	anthropicProvider  = AnthropicProvider
)

func modelCachePath() string {
	name := "models.json"
	if os.Getenv(endpointVariable) != "" {
		name = "models.sim.json"
	}
	return filepath.Join(os.Getenv("XDG_STATE_HOME"), name)
}

func chosenModel(providerName string, model string) (Choice, error) {
	return Chosen(modelCachePath(), providerName, model)
}

func parseModelSelection(selection string) (string, string, string, error) {
	return ParseSelection(modelCachePath(), selection)
}

func updateModelsWithoutProviderListings(output io.Writer, endpoint string, path string) error {
	return Update(output, endpoint, path, func(context.Context, string, string) ([]agent.Model, error) {
		return nil, nil
	})
}

func ensureModelsWithoutProviderListings(output io.Writer, endpoint string, path string) error {
	return Ensure(output, endpoint, path, func(context.Context, string, string) ([]agent.Model, error) {
		return nil, nil
	})
}
