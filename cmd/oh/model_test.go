package main

import (
	"strings"
	"testing"
)

func TestModelSelectionAcceptsQualifiedAndFuzzyNames(t *testing.T) {
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

func TestAmbiguousModelSelectionShowsEveryMatch(t *testing.T) {
	choices := []modelChoice{
		{provider: opencodeGoProvider, model: "deepseek-v4", efforts: []string{"high"}},
		{provider: opencodeGoProvider, model: "deepseek-v4-pro", efforts: []string{"high"}},
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
		{provider: opencodeGoProvider, model: "deepseek-v4", efforts: []string{"high"}},
		{provider: opencodeGoProvider, model: "deepseek-v4-pro", efforts: []string{"high"}},
	}

	choice, err := matchModel("deepseek-v4", choices)
	if err != nil {
		t.Fatal(err)
	}
	if choice.model != "deepseek-v4" {
		t.Errorf("got model %q", choice.model)
	}
}
