package models

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/internal/modelsdev"
)

const (
	cacheVersion  = 1
	updateTimeout = 90 * time.Second
)

const (
	CodexProvider      = "codex"
	OpencodeGoProvider = "opencode-go"
	AnthropicProvider  = "anthropic"
)

var registryNames = map[string]string{
	CodexProvider:      "openai",
	OpencodeGoProvider: "opencode-go",
	AnthropicProvider:  "anthropic",
}

func ProviderNames() []string {
	return []string{CodexProvider, OpencodeGoProvider, AnthropicProvider}
}

type modelCache struct {
	Version   int                     `json:"version"`
	Providers map[string]cachedModels `json:"providers"`
}

type cachedModels struct {
	Fetched time.Time     `json:"fetched"`
	Source  string        `json:"source"`
	Models  []agent.Model `json:"models"`
}

const (
	sourceEndpoint = "endpoint"
	sourceRegistry = "models.dev"
	sourceBoth     = "endpoint+models.dev"
)

func registryAddress(endpoint string) string {
	if endpoint == "" {
		return ""
	}

	address, err := url.Parse(endpoint)
	if err != nil || address.Scheme == "" || address.Host == "" {
		return ""
	}

	wanted := make([]string, 0, len(registryNames))
	for _, name := range registryNames {
		wanted = append(wanted, name)
	}

	sort.Strings(wanted)

	query := url.Values{simulatedRegistryQuery: {strings.Join(wanted, ",")}}

	return address.Scheme + "://" + address.Host + simulatedRegistryPath + "?" + query.Encode()
}

const (
	simulatedRegistryPath  = "/models.dev/api.json"
	simulatedRegistryQuery = "providers"
)

func loadModelCache(path string) modelCache {
	empty := modelCache{Version: cacheVersion, Providers: map[string]cachedModels{}}

	data, err := os.ReadFile(path) //nolint:gosec // the path is ours
	if err != nil {
		return empty
	}

	var cache modelCache
	if json.Unmarshal(data, &cache) != nil || cache.Version != cacheVersion {
		return empty
	}

	if cache.Providers == nil {
		cache.Providers = map[string]cachedModels{}
	}

	return cache
}

func saveModelCache(path string, cache modelCache) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}

	cache.Version = cacheVersion

	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o600)
}

func supplement(listed []agent.Model, registered map[string]agent.Model) []agent.Model {
	supplemented := make([]agent.Model, 0, len(listed))

	for _, model := range listed {
		known, found := registered[model.ID]
		if found {
			if model.Name == "" {
				model.Name = known.Name
			}
			if len(model.EffortLevels) == 0 {
				model.EffortLevels = slices.Clone(known.EffortLevels)
			}
			if model.ContextWindowTokens == 0 {
				model.ContextWindowTokens = known.ContextWindowTokens
			}
			if model.MaxOutputTokens == 0 {
				model.MaxOutputTokens = known.MaxOutputTokens
			}
		}

		supplemented = append(supplemented, model)
	}

	return supplemented
}

func fromRegistry(registered map[string]agent.Model) []agent.Model {
	models := make([]agent.Model, 0, len(registered))
	for _, model := range registered {
		models = append(models, model)
	}

	sort.Slice(models, func(first int, second int) bool {
		return models[first].ID < models[second].ID
	})

	return models
}

func choicesFor(providerName string, models []agent.Model) []Choice {
	choices := make([]Choice, 0, len(models))

	for _, model := range models {
		if model.ID == "" || len(model.EffortLevels) == 0 || model.MaxOutputTokens <= 0 {
			continue
		}

		choices = append(choices, Choice{
			Provider:            providerName,
			Model:               model.ID,
			EffortLevels:        model.EffortLevels,
			ContextWindowTokens: model.ContextWindowTokens,
			MaxOutputTokens:     model.MaxOutputTokens,
		})
	}

	return choices
}

func availableModelChoices(cache modelCache) []Choice {
	var available []Choice

	for _, providerName := range ProviderNames() {
		if listing, found := cache.Providers[providerName]; found {
			available = append(available, choicesFor(providerName, listing.Models)...)
		}
	}

	return available
}

func List(output io.Writer, path string) error {
	choices := availableModelChoices(loadModelCache(path))
	if len(choices) == 0 {
		return errors.New("no models are known: run with -u to fetch the model list")
	}

	for _, choice := range choices {
		if _, err := fmt.Fprintf(output, "%s/%s\n", choice.Provider, choice.Model); err != nil {
			return err
		}
	}

	return nil
}

type ProviderLister func(context.Context, string, string) ([]agent.Model, error)

func Update(output io.Writer, endpoint string, path string, listProviderModels ProviderLister) error {
	ctx, cancel := context.WithTimeout(context.Background(), updateTimeout)
	defer cancel()

	registry, err := modelsdev.Fetch(ctx, registryAddress(endpoint), nil)
	if err != nil {
		_, _ = fmt.Fprintf(output, "models.dev: %s\n", err)
		registry = modelsdev.Registry{}
	}

	cache := loadModelCache(path)

	var described int

	for _, providerName := range ProviderNames() {
		registered := registry.Provider(registryNames[providerName])

		models, source, why := describeProviderModels(ctx, providerName, endpoint, registered, listProviderModels)
		if len(models) == 0 {
			_, _ = fmt.Fprintf(output, "%-12s nothing to record: %s\n", providerName, why)

			continue
		}

		cache.Providers[providerName] = cachedModels{
			Fetched: time.Now(),
			Source:  source,
			Models:  models,
		}
		described++

		_, _ = fmt.Fprintf(output, "%-12s %3d models  %-19s %-14s %s\n",
			providerName, len(models), source, pickable(models), why)
	}

	if described == 0 {
		return errors.New("no provider could be described")
	}

	if err := saveModelCache(path, cache); err != nil {
		return err
	}

	_, _ = fmt.Fprintln(output, "Stored model list in "+path)

	return nil
}

func pickable(models []agent.Model) string {
	var count int
	for _, model := range models {
		if len(model.EffortLevels) > 0 {
			count++
		}
	}

	return fmt.Sprintf("%d selectable", count)
}

func describeProviderModels(
	ctx context.Context,
	providerName string,
	endpoint string,
	registered map[string]agent.Model,
	listProviderModels ProviderLister,
) ([]agent.Model, string, string) {
	listed, err := listProviderModels(ctx, providerName, endpoint)

	why := "the endpoint lists no models"
	if err != nil {
		why = err.Error()
	}

	if len(listed) > 0 {
		if len(registered) == 0 {
			return listed, sourceEndpoint, ""
		}

		return supplement(listed, registered), sourceBoth, ""
	}

	if len(registered) > 0 {
		return fromRegistry(registered), sourceRegistry, why
	}

	return nil, "", why
}
