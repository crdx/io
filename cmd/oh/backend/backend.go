package backend

import (
	"context"
	"fmt"

	"crdx.org/io/agent"
	"crdx.org/io/internal/req"
	"crdx.org/io/provider/anthropic"
	"crdx.org/io/provider/codex"
	"crdx.org/io/provider/opencodego"
	"crdx.org/io/tool"

	"crdx.org/io/cmd/oh/model"
)

const EndpointVariable = "OH_ENDPOINT_URL"

type EndpointSettings struct {
	OverrideURL string
	OllamaHost  string
}

const (
	standInToken           = "stand-in"
	listingModel           = "listing"
	listingEffort          = "high"
	listingMaxOutputTokens = 1
)

type Client interface {
	agent.Provider
	agent.State
	ObserveHTTP(observer req.Observer)
}

type Connection struct {
	Client

	ToolsSize func([]tool.Tool) int

	Search *codex.SearchClient
}

// ObserveHTTP watches the conversation and the searches made beside it alike.
func (self *Connection) ObserveHTTP(observer req.Observer) {
	self.Client.ObserveHTTP(observer)
	self.Search.ObserveHTTP(observer)
}

type sessionScoped interface {
	UseSession(name string)
}

func (self *Connection) UseSession(name string) {
	if scoped, isScoped := self.Client.(sessionScoped); isScoped {
		scoped.UseSession(name)
	}
}

func Connect(choice model.Choice, effort string, endpoints EndpointSettings) (*Connection, error) {
	if err := requireCredentials(choice.Provider, endpoints.OverrideURL); err != nil {
		return nil, err
	}

	connection, err := connectProvider(choice, effort, endpoints)
	if err != nil {
		return nil, err
	}

	if connection.Search == nil {
		tokens, address := codexCredentials(endpoints.OverrideURL)
		if connection.Search, err = newSearchClient(tokens, address); err != nil {
			return nil, err
		}
	}

	return connection, nil
}

func IsLoggedIn(providerName string) bool {
	return requireCredentials(providerName, "") == nil
}

func requireCredentials(providerName string, overrideURL string) error {
	if overrideURL != "" {
		return nil
	}

	switch providerName {
	case model.CodexProvider:
		_, err := codex.LoadStoredCredentials()

		return err
	case model.AnthropicProvider:
		_, err := anthropic.LoadStoredCredentials()

		return err
	case model.OpencodeGoProvider:
		_, err := opencodego.StoredKey()

		return err
	default:
		return nil
	}
}

func connectProvider(choice model.Choice, effort string, endpoints EndpointSettings) (*Connection, error) {
	switch choice.Provider {
	case model.CodexProvider:
		return connectCodex(choice, effort, endpoints.OverrideURL)
	case model.OpencodeGoProvider:
		return connectOpencodeGo(choice, effort, endpoints.OverrideURL)
	case model.AnthropicProvider:
		return connectAnthropic(choice, effort, endpoints.OverrideURL)
	case model.OllamaProvider:
		return connectOllama(choice, effort, endpoints)
	default:
		return nil, fmt.Errorf("unknown provider %q", choice.Provider)
	}
}

func ListModels(ctx context.Context, providerName string, endpoints EndpointSettings) ([]agent.Model, error) {
	if providerName == model.CodexProvider && endpoints.OverrideURL == "" {
		return nil, agent.ErrNoListing
	}

	choice := model.Choice{
		Provider:        providerName,
		ID:              listingModel,
		MaxOutputTokens: listingMaxOutputTokens,
	}
	client, err := connectProvider(choice, listingEffort, endpoints)
	if err != nil {
		return nil, err
	}

	lister, canList := client.Client.(agent.Lister)
	if !canList {
		return nil, agent.ErrNoListing
	}

	return lister.Models(ctx)
}

func Resolve(
	requested model.Selection,
	resumed model.Selection,
	configured []model.Selection,
	roundRobinPath string,
) (model.Selection, error) {
	if resumed != (model.Selection{}) {
		if requested.Provider != "" && requested.Provider != resumed.Provider {
			return model.Selection{}, fmt.Errorf(
				"cannot resume a %s session with %s", resumed.Provider, requested.Provider,
			)
		}
		if requested.Model == "" {
			return resumed, nil
		}
	}

	if requested.Model != "" {
		return requested, nil
	}

	return model.ReserveRoundRobin(roundRobinPath, configured)
}
