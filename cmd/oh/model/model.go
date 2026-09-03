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
	"crdx.org/io/cmd/oh/style"
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

type latestIteration struct {
	ID        string
	Iteration []int
}

func latestModelIterations(models []agent.Model) map[modelFamily]latestIteration {
	latest := make(map[modelFamily]latestIteration)

	for _, model := range models {
		family, iteration, hasIteration := getModelIteration(model.ID)
		if !hasIteration {
			continue
		}

		knownLatest, hasKnown := latest[family]
		if !hasKnown || slices.Compare(iteration, knownLatest.Iteration) > 0 {
			latest[family] = latestIteration{ID: model.ID, Iteration: iteration}
		}
	}

	return latest
}

func supersededBy(latest map[modelFamily]latestIteration, id string) (string, bool) {
	family, iteration, hasIteration := getModelIteration(id)
	if !hasIteration || slices.Equal(iteration, latest[family].Iteration) {
		return "", false
	}

	return latest[family].ID, true
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

const undrivableReason = "unsupported request shape"

type ignoredModel struct {
	Name   string
	Reason string
}

func recordableModels(providerName string, models []agent.Model) ([]agent.Model, []ignoredModel) {
	latest := latestModelIterations(models)

	recordable := make([]agent.Model, 0, len(models))

	var ignoredModels []ignoredModel

	for _, model := range models {
		supersedingID, isSuperseded := supersededBy(latest, model.ID)

		switch {
		case isSuperseded:
			ignoredModels = append(ignoredModels, ignoredFor(model, "superseded by "+supersedingID))
		case !isDrivable(providerName, model.ID):
			ignoredModels = append(ignoredModels, ignoredFor(model, undrivableReason))
		default:
			recordable = append(recordable, model)
		}
	}

	return recordable, ignoredModels
}

func unselectableReason(model agent.Model) string {
	switch {
	case model.ID == "":
		return "unknown id"
	case len(model.EffortLevels) == 0:
		return "unknown effort level"
	case model.MaxOutputTokens <= 0:
		return "unknown output limit"
	default:
		return ""
	}
}

const unnamedModelName = "(unnamed)"

func ignoredName(model agent.Model) string {
	switch {
	case model.ID != "":
		return model.ID
	case model.Name != "":
		return model.Name
	default:
		return unnamedModelName
	}
}

func ignoredFor(model agent.Model, reason string) ignoredModel {
	return ignoredModel{Name: ignoredName(model), Reason: reason}
}

func unselectableModels(models []agent.Model) []ignoredModel {
	var ignoredModels []ignoredModel

	for _, model := range models {
		if reason := unselectableReason(model); reason != "" {
			ignoredModels = append(ignoredModels, ignoredFor(model, reason))
		}
	}

	return ignoredModels
}

func choicesFor(providerName string, models []agent.Model) []Choice {
	choices := make([]Choice, 0, len(models))

	for _, model := range models {
		if unselectableReason(model) != "" || !isDrivable(providerName, model.ID) {
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

	_, _ = fmt.Fprintln(output, style.Subtle(refreshMessage))

	ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
	defer cancel()

	var reportedText bytes.Buffer

	err := updateModels(ctx, &reportedText, endpoint, path, listProviderModels, false)
	if err == nil {
		return nil
	}

	_, _ = io.Copy(output, &reportedText)

	if len(cache.Providers) == 0 {
		return err
	}

	_, _ = fmt.Fprintln(output, style.Change("model list not refreshed: %s", err))

	cache.CheckedAt = time.Now()

	return saveModelCache(path, cache)
}

func isCacheCurrent(cache modelCache, now time.Time) bool {
	if len(cache.Providers) == 0 || cache.CheckedAt.IsZero() {
		return false
	}

	return now.Sub(cache.CheckedAt) < maximumCacheAge
}

func Update(
	output io.Writer,
	endpoint string,
	path string,
	listProviderModels ProviderLister,
	isShowingIgnored bool,
) error {
	ctx, cancel := context.WithTimeout(context.Background(), updateTimeout)
	defer cancel()

	return updateModels(ctx, output, endpoint, path, listProviderModels, isShowingIgnored)
}

func updateModels(
	ctx context.Context,
	output io.Writer,
	endpoint string,
	path string,
	listProviderModels ProviderLister,
	isShowingIgnored bool,
) error {
	registry, err := modelsdev.Fetch(ctx, registryAddress(endpoint), nil)
	if err != nil {
		_, _ = fmt.Fprintln(output, style.Failure("models.dev: %s", err))
		registry = modelsdev.Registry{}
	}

	cache := loadModelCache(path)

	writeHeadings(output)

	var describedCount int

	reports := make([]providerReport, 0, len(ProviderNames()))

	for _, providerName := range ProviderNames() {
		registeredModels := registry.Provider(registryNames[providerName])

		listedModels, source, why := describeProviderModels(ctx, providerName, registeredModels, listProviderModels)

		models, ignoredModels := recordableModels(providerName, listedModels)
		ignoredModels = append(ignoredModels, unselectableModels(models)...)

		report := providerReport{
			Provider:        providerName,
			Source:          source,
			RecordedCount:   len(models),
			SelectableCount: pickable(models),
			IgnoredModels:   ignoredModels,
		}

		if len(models) == 0 {
			report.Why = nothingRecordedReason(why, ignoredModels)
		} else {
			cache.Providers[providerName] = cachedModels{
				FetchedAt: time.Now(),
				Source:    source,
				Models:    models,
			}
			describedCount++
		}

		reports = append(reports, report)
		writeProviderReport(output, report)
	}

	if isShowingIgnored {
		writeIgnoredModels(output, reports)
	}

	if describedCount == 0 {
		return errors.New("no provider could be described")
	}

	cache.CheckedAt = time.Now()

	if err := saveModelCache(path, cache); err != nil {
		return err
	}

	_, _ = fmt.Fprintln(output)
	_, _ = fmt.Fprintln(output, style.Subtle("Stored model list in ")+style.Normal(path))

	if !isShowingIgnored {
		writeIgnoredHint(output, reports)
	}

	return nil
}

func pickable(models []agent.Model) int {
	var count int
	for _, model := range models {
		if unselectableReason(model) == "" {
			count++
		}
	}

	return count
}

func nothingRecordedReason(why string, ignoredModels []ignoredModel) string {
	if why != "" {
		return why
	}

	if len(ignoredModels) == 1 {
		return "the one model it offers is ignored"
	}

	return fmt.Sprintf("all %d models it offers are ignored", len(ignoredModels))
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
