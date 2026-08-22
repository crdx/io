package main

import (
	"slices"
	"strings"
	"testing"

	"crdx.org/io/agent"
)

func useCachedModels(t *testing.T) {
	t.Helper()

	writeModelCache(t, modelCache{
		Version: cacheVersion,
		Providers: map[string]cachedModels{
			codexProvider: {Models: []agent.Model{{
				ID:              "gpt-5.6-sol",
				EffortLevels:    []string{"none", "low", "medium", "high", "xhigh", "max"},
				MaxOutputTokens: 128_000,
			}}},
			opencodeGoProvider: {Models: []agent.Model{{
				ID:              "deepseek-v4-pro",
				EffortLevels:    []string{"high", "max"},
				MaxOutputTokens: 384_000,
			}}},
			anthropicProvider: {Models: []agent.Model{{
				ID:              "claude-opus-5",
				EffortLevels:    []string{"low", "medium", "high", "xhigh", "max"},
				MaxOutputTokens: 128_000,
			}}},
		},
	})
}

func TestModelSelectionAcceptsQualifiedAndFuzzyNames(t *testing.T) {
	useCachedModels(t)

	for _, selection := range []string{
		"opencode-go/deepseek-v4-pro@high",
		"deepseek@hi",
		"deepseek-v4@high",
	} {
		provider, model, effort, err := parseModelSelection(selection)
		if err != nil {
			t.Errorf("%s: %v", selection, err)
			continue
		}
		if provider != opencodeGoProvider || model != "deepseek-v4-pro" || effort != "high" {
			t.Errorf("%s: got %s/%s@%s", selection, provider, model, effort)
		}
	}
}

func TestModelSelectionReachesAnthropic(t *testing.T) {
	useCachedModels(t)

	for _, selection := range []string{
		"anthropic/claude-opus-5@high",
		"opus@hi",
		"claude-opus-5@high",
	} {
		provider, model, effort, err := parseModelSelection(selection)
		if err != nil {
			t.Errorf("%s: %v", selection, err)
			continue
		}
		if provider != anthropicProvider || model != "claude-opus-5" || effort != "high" {
			t.Errorf("%s: got %s/%s@%s", selection, provider, model, effort)
		}
	}
}

func TestAnthropicOffersEveryEffortLevelTheModelTakes(t *testing.T) {
	useCachedModels(t)

	for _, effort := range []string{"low", "medium", "high", "xhigh", "max"} {
		_, _, resolved, err := parseModelSelection("anthropic/claude-opus-5@" + effort)
		if err != nil {
			t.Errorf("%s: %v", effort, err)
			continue
		}
		if resolved != effort {
			t.Errorf("expected %s, got %s", effort, resolved)
		}
	}
}

func listedModels() []modelChoice {
	return []modelChoice{
		{provider: anthropicProvider, model: "claude-opus-4-5"},
		{provider: anthropicProvider, model: "claude-opus-4-5-20251101"},
		{provider: anthropicProvider, model: "claude-opus-5"},
		{provider: anthropicProvider, model: "claude-sonnet-5"},
		{provider: codexProvider, model: "gpt-5.6-sol"},
	}
}

func TestACloserReadingOfAQueryWinsOutright(t *testing.T) {
	for query, want := range map[string]string{
		"anthropic/claude-opus-5":  "claude-opus-5",
		"claude-opus-5":            "claude-opus-5",
		"claude-opus-4-5":          "claude-opus-4-5",
		"opus-5":                   "claude-opus-5",
		"claude-opus-4-5-20251101": "claude-opus-4-5-20251101",
		"sonnet":                   "claude-sonnet-5",
		"sol":                      "gpt-5.6-sol",
		"gpt":                      "gpt-5.6-sol",
		"gp56":                     "gpt-5.6-sol",
	} {
		choice, err := matchModel(query, listedModels())
		if err != nil {
			t.Errorf("%s: %v", query, err)

			continue
		}

		if choice.model != want {
			t.Errorf("expected %s to find %s, got %s", query, want, choice.model)
		}
	}
}

func TestAQueryReadTheSameWayBySeveralModelsIsAmbiguous(t *testing.T) {
	_, err := matchModel("claude-opus", listedModels())
	if err == nil {
		t.Fatal("expected a query opening three model names to be refused")
	}

	if !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("expected the ambiguity to be named, got %v", err)
	}
}

func TestAModelOffersOnlyTheEffortLevelsItTakes(t *testing.T) {
	useCachedModels(t)

	wanted := map[string][]string{
		"gpt-5.6-sol":     {"none", "low", "medium", "high", "xhigh", "max"},
		"deepseek-v4-pro": {"high", "max"},
		"claude-opus-5":   {"low", "medium", "high", "xhigh", "max"},
	}

	for _, choice := range modelChoices() {
		want, known := wanted[choice.model]
		if !known {
			t.Errorf("no effort levels pinned for %s", choice.model)

			continue
		}

		if !slices.Equal(choice.effortLevels, want) {
			t.Errorf("expected %s to take %v, got %v", choice.model, want, choice.effortLevels)
		}
	}
}

func TestAnEffortTheModelDoesNotTakeIsRefused(t *testing.T) {
	useCachedModels(t)

	if _, _, _, err := parseModelSelection("sol@minimal"); err == nil {
		t.Error("expected an effort gpt-5.6-sol does not take to be refused before the turn")
	}
}

func TestOffAsksForNone(t *testing.T) {
	useCachedModels(t)

	for _, selection := range []string{"sol@off", "sol@of", "sol@o", "sol@none", "sol@n"} {
		_, _, effort, err := parseModelSelection(selection)
		if err != nil {
			t.Errorf("%s: %v", selection, err)

			continue
		}

		if effort != "none" {
			t.Errorf("expected %s to ask for none, got %q", selection, effort)
		}
	}
}

func TestOffIsNotOfferedToAModelThatCannotStopReasoning(t *testing.T) {
	useCachedModels(t)

	for _, selection := range []string{"opus@off", "deepseek@off"} {
		if _, _, _, err := parseModelSelection(selection); err == nil {
			t.Errorf("expected %s to be refused, since that model takes no none level", selection)
		}
	}
}

func TestAnEffortWrittenAsAnAliasInTheConfigIsResolved(t *testing.T) {
	_, _, effort, err := resolveProviderChoice(
		"", "", "",
		Config{Provider: codexProvider, Model: "gpt-5.6-sol", Effort: "off"},
		nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if effort != "none" {
		t.Errorf("expected the level rather than the word that asked for it, got %q", effort)
	}
}

func TestAmbiguousModelSelectionShowsEveryMatch(t *testing.T) {
	choices := []modelChoice{
		{provider: opencodeGoProvider, model: "deepseek-v4", effortLevels: []string{"high"}},
		{provider: opencodeGoProvider, model: "deepseek-v4-pro", effortLevels: []string{"high"}},
	}

	_, err := matchModel("deepseek", choices)
	if err == nil {
		t.Fatal("expected an ambiguous model to be rejected")
	}
	for _, match := range []string{"opencode-go/deepseek-v4", "opencode-go/deepseek-v4-pro"} {
		if !strings.Contains(err.Error(), match) {
			t.Errorf("error does not show %q: %v", match, err)
		}
	}
}

func TestExactModelSelectionWinsOverFuzzyMatches(t *testing.T) {
	choices := []modelChoice{
		{provider: opencodeGoProvider, model: "deepseek-v4", effortLevels: []string{"high"}},
		{provider: opencodeGoProvider, model: "deepseek-v4-pro", effortLevels: []string{"high"}},
	}

	choice, err := matchModel("deepseek-v4", choices)
	if err != nil {
		t.Fatal(err)
	}
	if choice.model != "deepseek-v4" {
		t.Errorf("got model %q", choice.model)
	}
}
