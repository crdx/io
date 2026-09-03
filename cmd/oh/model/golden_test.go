package model

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/internal/util/strutil"
)

const (
	goldenCachePath       = "/state/models.json"
	goldenRegistryAddress = `"<registry>"`
)

var updateGoldens = flag.Bool("update", false, "write what was drawn back to the golden files")

func TestAnUpdateWithNothingReachableMatchesTheGolden(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	var output bytes.Buffer
	err := Update(&output, deadAddress, modelCachePath(), unreachableProviders, true)
	if err == nil {
		t.Fatal("expected an update with nothing reachable to fail")
	}

	assertGolden(t, "update-nothing-reachable.ansi", strings.Join([]string{
		report(t, output.String()),
		"=== error ===\n", err.Error(), "\n",
	}, ""))
}

func TestAnUpdateFromTheRegistryAloneMatchesTheGolden(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	var output bytes.Buffer
	if err := Update(&output, serveRegistry(t, oneCodexModel), modelCachePath(), unreachableProviders, true); err != nil {
		t.Fatal(err)
	}

	assertGolden(t, "update-from-registry.ansi", report(t, output.String()))
}

func TestAnUpdateAProviderListsItselfMatchesTheGolden(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	listedByProvider := map[string][]agent.Model{
		OllamaProvider: {
			{ID: "gpt-5.6-sol", Name: "GPT-5.6 Sol", EffortLevels: []string{"low", "high"}, MaxOutputTokens: 32_000},
			{ID: "an-unselectable-model"},
		},
		CodexProvider:     {{ID: "gpt-5.6-sol"}},
		AnthropicProvider: nil,
	}

	var output bytes.Buffer
	lister := func(ctx context.Context, providerName string) ([]agent.Model, error) {
		if listed, isFound := listedByProvider[providerName]; isFound {
			return listed, nil
		}

		return unreachableProviders(ctx, providerName)
	}
	if err := Update(&output, serveRegistry(t, oneCodexModel), modelCachePath(), lister, true); err != nil {
		t.Fatal(err)
	}

	assertGolden(t, "update-listed-by-provider.ansi", report(t, output.String()))
}

func ignoredModelListings() map[string][]agent.Model {
	return map[string][]agent.Model{
		OpencodeGoProvider: {
			{ID: "grok-4.6", EffortLevels: []string{"high"}, MaxOutputTokens: 32_000},
			{ID: "qwen3-max", EffortLevels: []string{"high"}, MaxOutputTokens: 32_000},
		},
		AnthropicProvider: {
			{ID: "claude-sonnet-4-5", EffortLevels: []string{"high"}, MaxOutputTokens: 64_000},
			{ID: "claude-sonnet-4-6", EffortLevels: []string{"high"}, MaxOutputTokens: 64_000},
			{ID: "claude-opus-4-5", EffortLevels: []string{"high"}, MaxOutputTokens: 32_000},
		},
		OllamaProvider: {
			{ID: "llama-3", EffortLevels: []string{"medium"}, MaxOutputTokens: 8_000},
			{ID: "llama-4", EffortLevels: []string{"medium"}},
			{ID: "a-locally-built-model-with-a-very-long-name"},
			{Name: "Nothing Identifies This One"},
			{},
		},
	}
}

func listingIgnoredModels(t *testing.T) ProviderLister {
	t.Helper()

	listedByProvider := ignoredModelListings()

	return func(ctx context.Context, providerName string) ([]agent.Model, error) {
		if listed, isFound := listedByProvider[providerName]; isFound {
			return listed, nil
		}

		return unreachableProviders(ctx, providerName)
	}
}

func TestAnUpdateNamesEveryModelItIgnoresAndWhy(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	var output bytes.Buffer
	if err := Update(&output, serveRegistry(t, oneCodexModel), modelCachePath(), listingIgnoredModels(t), true); err != nil {
		t.Fatal(err)
	}

	assertGolden(t, "update-ignoring-models.ansi", report(t, output.String()))
}

func TestAnUpdateWithoutTheFlagCountsWhatItIgnoredAndSaysHowToSeeIt(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	var output bytes.Buffer
	if err := Update(&output, serveRegistry(t, oneCodexModel), modelCachePath(), listingIgnoredModels(t), false); err != nil {
		t.Fatal(err)
	}

	assertGolden(t, "update-counting-ignored.ansi", report(t, output.String()))
}

func TestAnUpdateWithoutColourKeepsItsColumns(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	var output bytes.Buffer
	restoreStyle := style.Init(&output)
	defer restoreStyle()

	if err := Update(&output, serveRegistry(t, oneCodexModel), modelCachePath(), listingIgnoredModels(t), true); err != nil {
		t.Fatal(err)
	}

	assertGolden(t, "update-ignoring-models-plain.txt", report(t, output.String()))
}

func TestAProviderWhoseModelsAreAllIgnoredSaysSoAndOneIgnoredModelReadsAsOne(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	var output bytes.Buffer
	lister := func(ctx context.Context, providerName string) ([]agent.Model, error) {
		if providerName == AnthropicProvider {
			return []agent.Model{{ID: "claude-opus-4-5", EffortLevels: []string{"high"}}}, nil
		}

		return unreachableProviders(ctx, providerName)
	}
	if err := Update(&output, serveRegistry(t, oneCodexModel), modelCachePath(), lister, false); err != nil {
		t.Fatal(err)
	}

	assertGolden(t, "update-ignoring-every-model.ansi", report(t, output.String()))
}

func TestAnUpdateThatRecordsNothingStillNamesWhatItIgnored(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	var output bytes.Buffer
	lister := func(ctx context.Context, providerName string) ([]agent.Model, error) {
		if providerName == OpencodeGoProvider {
			return []agent.Model{
				{ID: "grok-4.6", EffortLevels: []string{"high"}, MaxOutputTokens: 32_000},
				{ID: "minimax-m2", EffortLevels: []string{"high"}, MaxOutputTokens: 32_000},
			}, nil
		}

		return unreachableProviders(ctx, providerName)
	}

	err := Update(&output, deadAddress, modelCachePath(), lister, true)
	if err == nil {
		t.Fatal("expected an update that recorded nothing to fail")
	}

	assertGolden(t, "update-ignoring-everything.ansi", strings.Join([]string{
		report(t, output.String()),
		"=== error ===\n", err.Error(), "\n",
	}, ""))
}

func TestAStartupRefreshThatRecordsNothingShowsWhatItIgnored(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	writeCheckedModelCache(t, time.Now().Add(-8*24*time.Hour))

	var output bytes.Buffer
	lister := func(ctx context.Context, providerName string) ([]agent.Model, error) {
		if providerName == OpencodeGoProvider {
			return []agent.Model{
				{ID: "grok-4.6", EffortLevels: []string{"high"}, MaxOutputTokens: 32_000},
				{ID: "minimax-m2", EffortLevels: []string{"high"}, MaxOutputTokens: 32_000},
			}, nil
		}

		return unreachableProviders(ctx, providerName)
	}
	if err := Ensure(&output, deadAddress, modelCachePath(), lister); err != nil {
		t.Fatalf("expected a failed refresh to be forgiven, got %v", err)
	}

	assertGolden(t, "ensure-refresh-ignoring-every-model.ansi", report(t, output.String()))
}

func unreachableProviders(_ context.Context, providerName string) ([]agent.Model, error) {
	switch providerName {
	case CodexProvider:
		return nil, agent.ErrNoListing
	case OpencodeGoProvider:
		return nil, errors.New("not logged in to OpenCode Go: run the login command with opencode-go")
	case AnthropicProvider:
		return nil, errors.New("not logged in to Anthropic: run the login command with anthropic")
	default:
		return nil, errors.New(`Get "http://localhost:11434/api/tags": connect: connection refused`)
	}
}

var registryAddressPattern = regexp.MustCompile(`"[^"]*` + regexp.QuoteMeta(simulatedRegistryPath) + `[^"]*"`)

func report(t *testing.T, drawn string) string {
	t.Helper()

	drawn = strings.ReplaceAll(drawn, modelCachePath(), goldenCachePath)
	drawn = registryAddressPattern.ReplaceAllString(drawn, goldenRegistryAddress)

	return strutil.VisibleEscapes(drawn)
}

func assertGolden(t *testing.T, name string, drawn string) {
	t.Helper()

	goldenPath := filepath.Join("testdata", name)
	if *updateGoldens {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, []byte(drawn), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath) //nolint:gosec // fixed testdata path
	if err != nil {
		t.Fatal(err)
	}
	if drawn != string(want) {
		t.Errorf("output differs from %s\n--- got ---\n%s--- want ---\n%s", goldenPath, drawn, want)
	}
}
