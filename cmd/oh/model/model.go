package model

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/internal/modelsdev"
	"crdx.org/io/provider/anthropic"
	"crdx.org/io/provider/codex"
	"crdx.org/io/provider/opencodego"
)

const refreshMessage = "Refreshing the model list..."

const (
	cacheVersion    = 3
	updateTimeout   = 90 * time.Second
	refreshTimeout  = 20 * time.Second
	maximumCacheAge = 7 * 24 * time.Hour
)

const (
	CodexProvider      = "codex"
	OpencodeGoProvider = "opencode-go"
	AnthropicProvider  = "anthropic"
	OllamaProvider     = "ollama"
)

var registryNames = map[string]string{
	CodexProvider:      "openai",
	OpencodeGoProvider: "opencode-go",
	AnthropicProvider:  "anthropic",
}

func ProviderNames() []string {
	return []string{CodexProvider, OpencodeGoProvider, AnthropicProvider, OllamaProvider}
}

func LoginProviderNames() []string {
	return []string{CodexProvider, OpencodeGoProvider, AnthropicProvider}
}

type modelCache struct {
	Version   int                     `json:"version"`
	CheckedAt time.Time               `json:"checked"`
	Providers map[string]cachedModels `json:"providers"`
}

type cachedModels struct {
	FetchedAt time.Time     `json:"fetched"`
	Source    string        `json:"source"`
	Models    []agent.Model `json:"models"`
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

	wantedNames := make([]string, 0, len(registryNames))
	for _, name := range registryNames {
		wantedNames = append(wantedNames, name)
	}

	sort.Strings(wantedNames)

	query := url.Values{simulatedRegistryQuery: {strings.Join(wantedNames, ",")}}

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

func supplement(listedModels []agent.Model, registeredModels map[string]agent.Model) []agent.Model {
	supplementedModels := make([]agent.Model, 0, len(listedModels))

	for _, model := range listedModels {
		knownModel, isFound := registeredModels[model.ID]
		if isFound {
			if model.Name == "" {
				model.Name = knownModel.Name
			}
			if len(model.EffortLevels) == 0 {
				model.EffortLevels = slices.Clone(knownModel.EffortLevels)
			}
			if model.ContextWindowTokens == 0 {
				model.ContextWindowTokens = knownModel.ContextWindowTokens
			}
			if model.MaxOutputTokens == 0 {
				model.MaxOutputTokens = knownModel.MaxOutputTokens
			}
		}

		supplementedModels = append(supplementedModels, model)
	}

	return supplementedModels
}

func fromRegistry(registeredModels map[string]agent.Model) []agent.Model {
	models := make([]agent.Model, 0, len(registeredModels))
	for _, model := range registeredModels {
		models = append(models, model)
	}

	sort.Slice(models, func(first int, second int) bool {
		return models[first].ID < models[second].ID
	})

	return models
}

var modelIterationPattern = regexp.MustCompile(`[0-9]+(?:[.-][0-9]+)*`)

type modelFamily struct {
	Prefix string
	Suffix string
}

func getModelIteration(id string) (modelFamily, []int, bool) {
	location := modelIterationPattern.FindStringIndex(id)
	if location == nil {
		return modelFamily{}, nil, false
	}

	family := modelFamily{
		Prefix: strings.TrimRight(id[:location[0]], "-."),
		Suffix: strings.TrimLeft(id[location[1]:], "-."),
	}

	parts := strings.FieldsFunc(id[location[0]:location[1]], func(character rune) bool {
		return character == '.' || character == '-'
	})
	iteration := make([]int, len(parts))
	for i, part := range parts {
		iteration[i], _ = strconv.Atoi(part)
	}

	return family, iteration, true
}

func latestModelIterations(models []agent.Model) []agent.Model {
	latest := make(map[modelFamily][]int)
	for _, model := range models {
		family, iteration, hasIteration := getModelIteration(model.ID)
		knownIteration, hasKnown := latest[family]
		if hasIteration && (!hasKnown || slices.Compare(iteration, knownIteration) > 0) {
			latest[family] = iteration
		}
	}

	retainedModels := make([]agent.Model, 0, len(latest))
	for _, model := range models {
		family, iteration, hasIteration := getModelIteration(model.ID)
		if !hasIteration || slices.Equal(iteration, latest[family]) {
			retainedModels = append(retainedModels, model)
		}
	}

	return retainedModels
}

func isDrivable(providerName string, id string) bool {
	switch providerName {
	case AnthropicProvider:
		return anthropic.SupportsAdaptiveThinking(id)
	case CodexProvider:
		return codex.SupportsResponses(id)
	case OpencodeGoProvider:
		return opencodego.SupportsCompletions(id)
	default:
		return true
	}
}

func drivableModels(providerName string, models []agent.Model) []agent.Model {
	retainedModels := make([]agent.Model, 0, len(models))
	for _, model := range models {
		if isDrivable(providerName, model.ID) {
			retainedModels = append(retainedModels, model)
		}
	}

	return retainedModels
}

func choicesFor(providerName string, models []agent.Model) []Choice {
	choices := make([]Choice, 0, len(models))

	for _, model := range models {
		if model.ID == "" || len(model.EffortLevels) == 0 || model.MaxOutputTokens <= 0 {
			continue
		}

		if !isDrivable(providerName, model.ID) {
			continue
		}

		choices = append(choices, Choice{
			Provider:            providerName,
			ID:                  model.ID,
			Name:                model.Name,
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
		if listing, isFound := cache.Providers[providerName]; isFound {
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
		if _, err := fmt.Fprintf(output, "%s/%s\n", choice.Provider, choice.ID); err != nil {
			return err
		}
	}

	return nil
}

type ProviderLister func(context.Context, string) ([]agent.Model, error)

func Ensure(output io.Writer, endpoint string, path string, listProviderModels ProviderLister) error {
	cache := loadModelCache(path)
	if isCacheCurrent(cache, time.Now()) {
		return nil
	}

	_, _ = fmt.Fprintln(output, refreshMessage)

	ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
	defer cancel()

	var reportedText bytes.Buffer

	err := updateModels(ctx, &reportedText, endpoint, path, listProviderModels)
	if err == nil {
		return nil
	}

	_, _ = io.Copy(output, &reportedText)

	if len(cache.Providers) == 0 {
		return err
	}

	_, _ = fmt.Fprintf(output, "model list not refreshed: %s\n", err)

	cache.CheckedAt = time.Now()

	return saveModelCache(path, cache)
}

func isCacheCurrent(cache modelCache, now time.Time) bool {
	if len(cache.Providers) == 0 || cache.CheckedAt.IsZero() {
		return false
	}

	return now.Sub(cache.CheckedAt) < maximumCacheAge
}

func Update(output io.Writer, endpoint string, path string, listProviderModels ProviderLister) error {
	ctx, cancel := context.WithTimeout(context.Background(), updateTimeout)
	defer cancel()

	return updateModels(ctx, output, endpoint, path, listProviderModels)
}

func updateModels(
	ctx context.Context,
	output io.Writer,
	endpoint string,
	path string,
	listProviderModels ProviderLister,
) error {
	registry, err := modelsdev.Fetch(ctx, registryAddress(endpoint), nil)
	if err != nil {
		_, _ = fmt.Fprintf(output, "models.dev: %s\n", err)
		registry = modelsdev.Registry{}
	}

	cache := loadModelCache(path)

	var describedCount int

	for _, providerName := range ProviderNames() {
		registeredModels := registry.Provider(registryNames[providerName])

		models, source, why := describeProviderModels(ctx, providerName, registeredModels, listProviderModels)
		models = latestModelIterations(models)
		models = drivableModels(providerName, models)
		if len(models) == 0 {
			_, _ = fmt.Fprintf(output, "%-12s nothing to record: %s\n", providerName, why)

			continue
		}

		cache.Providers[providerName] = cachedModels{
			FetchedAt: time.Now(),
			Source:    source,
			Models:    models,
		}
		describedCount++

		_, _ = fmt.Fprintf(output, "%-12s %3d models  %-19s %s\n",
			providerName, len(models), source, pickable(models))
	}

	if describedCount == 0 {
		return errors.New("no provider could be described")
	}

	cache.CheckedAt = time.Now()

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
	registeredModels map[string]agent.Model,
	listProviderModels ProviderLister,
) ([]agent.Model, string, string) {
	listedModels, err := listProviderModels(ctx, providerName)

	why := "the endpoint lists no models"
	if err != nil {
		why = err.Error()
	}

	if len(listedModels) > 0 {
		if len(registeredModels) == 0 {
			return listedModels, sourceEndpoint, ""
		}

		return supplement(listedModels, registeredModels), sourceBoth, ""
	}

	if len(registeredModels) > 0 {
		return fromRegistry(registeredModels), sourceRegistry, why
	}

	return nil, "", why
}
