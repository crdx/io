package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"crdx.org/io/agent"
	"crdx.org/io/internal/sim"
)

func writeModelCache(t *testing.T, cache modelCache) {
	t.Helper()

	t.Setenv("XDG_STATE_HOME", t.TempDir())

	if err := saveModelCache(modelCachePath(), cache); err != nil {
		t.Fatal(err)
	}
}

func TestOnlyTheCachedListingDescribesAProvider(t *testing.T) {
	cache := modelCache{
		Version: cacheVersion,
		Providers: map[string]cachedModels{
			anthropicProvider: {Models: []agent.Model{
				{ID: "claude-sonnet-5", EffortLevels: []string{"low", "high"}, MaxOutputTokens: 128_000},
				{ID: "claude-haiku-4-5", EffortLevels: []string{"low"}, MaxOutputTokens: 64_000},
			}},
		},
	}

	available := availableModelChoices(cache)

	var models []string
	for _, choice := range available {
		if choice.provider != anthropicProvider {
			t.Errorf("expected nothing from %s, got %s", choice.provider, choice.model)
		}
		models = append(models, choice.model)
	}

	want := []string{"claude-sonnet-5", "claude-haiku-4-5"}
	if !slices.Equal(models, want) {
		t.Errorf("expected the cached listing alone, got %v", models)
	}
}

func TestAnEmptyCacheOffersNothingToSelect(t *testing.T) {
	available := availableModelChoices(modelCache{Version: cacheVersion, Providers: map[string]cachedModels{}})

	if len(available) != 0 {
		t.Fatalf("expected nothing before an update, got %v", available)
	}

	if _, err := matchModel("anything", available); err == nil {
		t.Error("expected selecting against an empty cache to say so")
	}
}

func TestAModelTakingNoEffortLevelCannotBeSelected(t *testing.T) {
	choices := choicesFor(codexProvider, []agent.Model{
		{ID: "gpt-5.6-sol", EffortLevels: []string{"high"}, MaxOutputTokens: 128_000},
		{ID: "chatgpt-image-latest"},
		{ID: "", EffortLevels: []string{"high"}, MaxOutputTokens: 128_000},
	})

	if len(choices) != 1 || choices[0].model != "gpt-5.6-sol" {
		t.Errorf("expected only what can be asked to think, got %v", choices)
	}
}

func TestTheRegistryFillsInWhatAListingLeftOut(t *testing.T) {
	listed := []agent.Model{
		{ID: "claude-opus-5", EffortLevels: []string{"high"}},
		{ID: "claude-sonnet-5"},
		{ID: "unknown-model"},
	}

	registered := map[string]agent.Model{
		"claude-opus-5": {
			ID:                  "claude-opus-5",
			Name:                "Claude Opus 5",
			EffortLevels:        []string{"low", "medium", "high", "xhigh", "max"},
			ContextWindowTokens: 274_000,
			MaxOutputTokens:     128_000,
		},
		"claude-sonnet-5": {
			ID:           "claude-sonnet-5",
			Name:         "Claude Sonnet 5",
			EffortLevels: []string{"low", "high"},
		},
	}

	supplemented := supplement(listed, registered)

	if supplemented[0].Name != "Claude Opus 5" || supplemented[0].ContextWindowTokens != 274_000 ||
		supplemented[0].MaxOutputTokens != 128_000 {
		t.Errorf("expected the registry to fill in the gaps, got %+v", supplemented[0])
	}

	if !slices.Equal(supplemented[0].EffortLevels, []string{"high"}) {
		t.Errorf("expected what the endpoint said to stand, got %v", supplemented[0].EffortLevels)
	}

	if !slices.Equal(supplemented[1].EffortLevels, []string{"low", "high"}) {
		t.Errorf("expected the registry to supply the missing efforts, got %v", supplemented[1].EffortLevels)
	}

	if supplemented[2].ID != "unknown-model" || supplemented[2].Name != "" {
		t.Errorf("expected a model the registry does not know to be left alone, got %+v", supplemented[2])
	}
}

func TestModelSelectionResolvesAgainstTheCachedListing(t *testing.T) {
	writeModelCache(t, modelCache{
		Version: cacheVersion,
		Providers: map[string]cachedModels{
			anthropicProvider: {Models: []agent.Model{
				{ID: "claude-sonnet-5", EffortLevels: []string{"low", "medium", "high"}, MaxOutputTokens: 128_000},
			}},
		},
	})

	provider, model, effort, err := parseModelSelection("sonnet@hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if provider != anthropicProvider || model != "claude-sonnet-5" || effort != "high" {
		t.Errorf("got %s/%s@%s", provider, model, effort)
	}
}

func TestTheOutputCeilingComesFromTheChosenModel(t *testing.T) {
	writeModelCache(t, modelCache{
		Version: cacheVersion,
		Providers: map[string]cachedModels{
			anthropicProvider: {Models: []agent.Model{
				{ID: "claude-opus-5", EffortLevels: []string{"high"}, MaxOutputTokens: 128_000},
				{ID: "claude-haiku-4-5", EffortLevels: []string{"high"}, MaxOutputTokens: 64_000},
			}},
		},
	})

	for model, want := range map[string]int{"claude-opus-5": 128_000, "claude-haiku-4-5": 64_000} {
		choice, err := chosenModel(anthropicProvider, model)
		if err != nil {
			t.Fatalf("%s: %v", model, err)
		}

		if choice.maxOutputTokens != want {
			t.Errorf("expected %s to allow %d, got %d", model, want, choice.maxOutputTokens)
		}
	}
}

func TestAModelNothingIsKnownAboutIsRefusedWithSomethingToDoAboutIt(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	_, err := chosenModel(anthropicProvider, "claude-from-the-future")
	if err == nil || !strings.Contains(err.Error(), "-u") {
		t.Errorf("expected the refusal to say what to do, got %v", err)
	}
}

func TestAnEffortACachedModelDoesNotTakeIsRefused(t *testing.T) {
	writeModelCache(t, modelCache{
		Version: cacheVersion,
		Providers: map[string]cachedModels{
			anthropicProvider: {Models: []agent.Model{
				{ID: "claude-haiku-4-5", EffortLevels: []string{"low", "medium"}, MaxOutputTokens: 64_000},
			}},
		},
	})

	if _, _, _, err := parseModelSelection("haiku@max"); err == nil {
		t.Error("expected an effort the model does not take to be refused")
	}
}

func TestACacheInAnotherFormatIsIgnored(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	path := modelCachePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"version":99,"providers":{"anthropic":{}}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := loadModelCache(path); len(got.Providers) != 0 {
		t.Errorf("expected a cache in another format to be ignored, got %v", got)
	}

	if _, _, _, err := parseModelSelection("opus@hi"); err == nil {
		t.Error("expected a cache in another format to leave nothing selectable")
	}
}

const deadAddress = "http://127.0.0.1:1"

func serveRegistry(t *testing.T, body string) string {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			if request.URL.Path != simulatedRegistryPath {
				http.Error(writer, "nothing here", http.StatusNotFound)

				return
			}

			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(body))
		},
	))

	t.Cleanup(server.Close)

	return server.URL + "/v1/messages"
}

func TestUpdatingWithNothingReachableSaysSo(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	var output bytes.Buffer
	if err := updateModels(&output, deadAddress, modelCachePath()); err == nil {
		t.Fatalf("expected the update to fail, got output %q", output.String())
	}
}

func TestUpdatingAgainstAStandInEndpointDescribesEveryProvider(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	endpoint := sim.New(&sim.Scenario{Model: "fake", Turns: []sim.Turn{{Say: "Hello."}}})
	server := httptest.NewServer(endpoint)

	t.Cleanup(server.Close)

	address := endpoint.Addresses(server.URL)[sim.Messages]
	if address == "" {
		t.Fatal("expected the Messages API to be served")
	}

	var output bytes.Buffer
	if err := updateModels(&output, address, modelCachePath()); err != nil {
		t.Fatalf("unexpected error: %v, output %q", err, output.String())
	}

	cache := loadModelCache(modelCachePath())

	for _, providerName := range providerNames {
		choices := choicesFor(providerName, cache.Providers[providerName].Models)
		if len(choices) != 1 || choices[0].model != "fake" {
			t.Errorf("expected %s to offer the scenario's model, got %v", providerName, choices)

			continue
		}

		if choices[0].maxOutputTokens <= 0 {
			t.Errorf("expected %s to know what the model may write, got %v", providerName, choices[0])
		}
	}
}

func TestTheRegistryIsReadFromAStandInEndpointWhenOneIsNamed(t *testing.T) {
	if got := registryAddress(""); got != "" {
		t.Errorf("expected the real registry when no endpoint is named, got %q", got)
	}

	want := "http://localhost:8080" + simulatedRegistryPath +
		"?providers=anthropic%2Copenai%2Copencode-go"

	if got := registryAddress("http://localhost:8080/v1/messages"); got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestTheRealRegistryIsNeverAskedAboutOurProviderNames(t *testing.T) {
	for _, endpoint := range []string{"", "not a url at all", "://broken"} {
		if got := registryAddress(endpoint); got != "" {
			t.Errorf("expected nothing for %q, got %q", endpoint, got)
		}
	}
}

func TestAStandInEndpointKeepsAModelCacheOfItsOwn(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	real := modelCachePath()

	t.Setenv(endpointVariable, "http://localhost:8080/v1/messages")

	if standIn := modelCachePath(); standIn == real {
		t.Errorf("expected a cache of its own, got %q for both", standIn)
	}
}

func TestAProviderThatListsNothingIsDescribedByTheRegistryAlone(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	endpoint := serveRegistry(t, `{
		"openai": {"models": {
			"gpt-5.6-sol": {
				"id": "gpt-5.6-sol", "name": "GPT-5.6 Sol", "reasoning": true,
				"reasoning_options": [{"type": "effort", "values": ["low", "high", "max"]}],
				"limit": {"context": 400000, "output": 128000}
			},
			"chatgpt-image-latest": {"id": "chatgpt-image-latest", "name": "Image"}
		}}
	}`)

	var output bytes.Buffer
	if err := updateModels(&output, endpoint, modelCachePath()); err != nil {
		t.Fatalf("unexpected error: %v, output %q", err, output.String())
	}

	cached := loadModelCache(modelCachePath()).Providers[codexProvider]
	if cached.Source != sourceRegistry {
		t.Errorf("expected the registry to stand in, got source %q", cached.Source)
	}

	if len(cached.Models) != 2 {
		t.Fatalf("expected both models to be recorded, got %v", cached.Models)
	}

	choices := choicesFor(codexProvider, cached.Models)
	if len(choices) != 1 || choices[0].model != "gpt-5.6-sol" {
		t.Fatalf("expected only the one that reasons to be selectable, got %v", choices)
	}

	if !slices.Equal(choices[0].effortLevels, []string{"low", "high", "max"}) {
		t.Errorf("expected the registry's effort levels, got %v", choices[0].effortLevels)
	}
}
