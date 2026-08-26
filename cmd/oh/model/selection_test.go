package model

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

func TestASelectionWithoutAnEffortSettlesNearestMedium(t *testing.T) {
	useCachedModels(t)

	for selection, want := range map[string]string{
		"anthropic/claude-opus-5": "medium",
		"deepseek":                "high",
		"sol":                     "medium",
	} {
		_, _, effort, err := parseModelSelection(selection)
		if err != nil {
			t.Errorf("%s: %v", selection, err)
			continue
		}
		if effort != want {
			t.Errorf("expected %s to select %s, got %s", selection, want, effort)
		}
	}
}

func TestASelectionWithAnEmptyEffortIsRefused(t *testing.T) {
	useCachedModels(t)

	if _, _, _, err := parseModelSelection("sol@"); err == nil {
		t.Error("expected a selection naming no effort after @ to be refused")
	}
}

func listedModels() []Choice {
	return []Choice{
		{Provider: anthropicProvider, Model: "claude-opus-4-5"},
		{Provider: anthropicProvider, Model: "claude-opus-4-5-20251101"},
		{Provider: anthropicProvider, Model: "claude-opus-5"},
		{Provider: anthropicProvider, Model: "claude-sonnet-5"},
		{Provider: codexProvider, Model: "gpt-5.6-sol"},
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

		if choice.Model != want {
			t.Errorf("expected %s to find %s, got %s", query, want, choice.Model)
		}
	}
}

func TestABareQueryBorrowsNoLettersFromTheProviderName(t *testing.T) {
	choices := []Choice{
		{Provider: anthropicProvider, Model: "claude-opus-5"},
		{Provider: anthropicProvider, Model: "claude-sonnet-5"},
	}

	choice, err := matchModel("opus5", choices)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if choice.Model != "claude-opus-5" {
		t.Errorf("got %s", choice.Model)
	}
}

func TestAQualifiedQueryIsStillReadLoosely(t *testing.T) {
	choices := []Choice{
		{Provider: opencodeGoProvider, Model: "deepseek-v4-pro"},
		{Provider: anthropicProvider, Model: "claude-opus-5"},
	}

	choice, err := matchModel("opencode/deepseek", choices)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if choice.Model != "deepseek-v4-pro" {
		t.Errorf("got %s", choice.Model)
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

	for _, choice := range Choices(modelCachePath()) {
		want, known := wanted[choice.Model]
		if !known {
			t.Errorf("no effort levels pinned for %s", choice.Model)

			continue
		}

		if !slices.Equal(choice.EffortLevels, want) {
			t.Errorf("expected %s to take %v, got %v", choice.Model, want, choice.EffortLevels)
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

func TestAmbiguousModelSelectionShowsEveryMatch(t *testing.T) {
	choices := []Choice{
		{Provider: opencodeGoProvider, Model: "deepseek-v4", EffortLevels: []string{"high"}},
		{Provider: opencodeGoProvider, Model: "deepseek-v4-pro", EffortLevels: []string{"high"}},
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

func TestAQueryScatteredThroughANameIsReadOnlyWhenGuessingIsAllowed(t *testing.T) {
	if matches := RankedChoices("gp56", listedModels()); len(matches) != 1 {
		t.Errorf("expected a loose reading to find the model, got %v", matches)
	}

	if matches := RankedChoicesWithoutGuessing("gp56", listedModels()); matches != nil {
		t.Errorf("expected the scattered query to name nothing, got %v", matches)
	}

	if matches := RankedChoicesWithoutGuessing("sonnet", listedModels()); len(matches) != 1 {
		t.Errorf("expected a query held within a name to still be read, got %v", matches)
	}
}

func TestExactModelSelectionWinsOverFuzzyMatches(t *testing.T) {
	choices := []Choice{
		{Provider: opencodeGoProvider, Model: "deepseek-v4", EffortLevels: []string{"high"}},
		{Provider: opencodeGoProvider, Model: "deepseek-v4-pro", EffortLevels: []string{"high"}},
	}

	choice, err := matchModel("deepseek-v4", choices)
	if err != nil {
		t.Fatal(err)
	}
	if choice.Model != "deepseek-v4" {
		t.Errorf("got model %q", choice.Model)
	}
}
