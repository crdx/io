package backend

import (
	"context"
	"fmt"

	"crdx.org/io/agent"
	"crdx.org/io/internal/req"
	"crdx.org/io/provider/codex"
	"crdx.org/io/tool"

	"crdx.org/io/cmd/oh/model"
)

const EndpointVariable = "OH_ENDPOINT_URL"

const (
	standInToken           = "stand-in"
	listingModel           = "listing"
	listingEffort          = "high"
	listingMaxOutputTokens = 1
)

type Client interface {
	agent.Provider
	agent.State
	ObserveHTTP(req.Observer)
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

func Connect(choice model.Choice, effort string, endpoint string) (*Connection, error) {
	connection, err := connectProvider(choice, effort, endpoint)
	if err != nil {
		return nil, err
	}

	if connection.Search == nil {
		tokens, address := codexCredentials(endpoint)
		if connection.Search, err = newSearchClient(tokens, address); err != nil {
			return nil, err
		}
	}

	return connection, nil
}

func connectProvider(choice model.Choice, effort string, endpoint string) (*Connection, error) {
	switch choice.Provider {
	case model.CodexProvider:
		return connectCodex(choice, effort, endpoint)
	case model.OpencodeGoProvider:
		return connectOpencodeGo(choice, effort, endpoint)
	case model.AnthropicProvider:
		return connectAnthropic(choice, effort, endpoint)
	default:
		return nil, fmt.Errorf("unknown provider %q", choice.Provider)
	}
}

func ListModels(ctx context.Context, providerName string, endpoint string) ([]agent.Model, error) {
	if providerName == model.CodexProvider && endpoint == "" {
		return nil, nil
	}

	choice := model.Choice{
		Provider:        providerName,
		Model:           listingModel,
		MaxOutputTokens: listingMaxOutputTokens,
	}
	client, err := connectProvider(choice, listingEffort, endpoint)
	if err != nil {
		return nil, err
	}

	lister, canList := client.Client.(agent.Lister)
	if !canList {
		return nil, nil
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
