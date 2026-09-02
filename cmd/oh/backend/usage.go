package backend

import (
	"crdx.org/io/agent"
	"crdx.org/io/provider/anthropic"
	"crdx.org/io/provider/opencodego"

	"crdx.org/io/cmd/oh/model"
)

const (
	anthropicLabel  = "Anthropic"
	openAILabel     = "OpenAI"
	opencodeGoLabel = "OpenCode Go"
)

type UsageSource struct {
	Provider string
	Label    string
	Reporter agent.UsageReporter

	HasIdleSessionWindow bool
}

func UsageSources() []UsageSource {
	return []UsageSource{
		{
			Provider:             model.AnthropicProvider,
			Label:                anthropicLabel,
			Reporter:             anthropicUsage(),
			HasIdleSessionWindow: true,
		},
		{
			Provider: model.CodexProvider,
			Label:    openAILabel,
		},
		{
			Provider: model.OpencodeGoProvider,
			Label:    opencodeGoLabel,
			Reporter: opencodeGoUsage(),
		},
	}
}

func anthropicUsage() agent.UsageReporter {
	if !IsLoggedIn(model.AnthropicProvider) {
		return nil
	}

	client, err := anthropic.New(
		anthropic.StoredCredentials(), listingModel, listingEffort, listingMaxOutputTokens,
	)
	if err != nil {
		return nil
	}

	return client
}

func opencodeGoUsage() agent.UsageReporter {
	key, err := opencodego.StoredKey()
	if err != nil {
		return nil
	}

	client, err := opencodego.New(
		opencodego.EndpointURL, key, listingModel, listingEffort, listingMaxOutputTokens,
	)
	if err != nil {
		return nil
	}

	client.UsageURL = opencodego.UsageEndpointURL

	return client
}
