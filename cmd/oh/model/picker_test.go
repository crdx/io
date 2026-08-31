package model

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"crdx.org/io/agent"
)

func TestTheEffortsOfferedRunFromLeastToMost(t *testing.T) {
	got := orderedEfforts([]string{"high", "none", "medium", "low"})
	want := []string{"none", "low", "medium", "high"}

	if !slices.Equal(got, want) {
		t.Errorf("expected the efforts in order, got %v", got)
	}
}

func TestAModelIsOfferedAtTheEffortNearestTheOneWanted(t *testing.T) {
	offers := offered([]Choice{
		{Provider: "codex", ID: "gpt-5.3-codex", EffortLevels: []string{"xhigh", "low", "medium"}},
		{Provider: "ollama", ID: "qwen3-coder:30b", EffortLevels: []string{"none"}},
		{Provider: "ollama", ID: "unlevelled", EffortLevels: []string{"whatever"}},
	}, "high")

	if offers[0].Effort != "xhigh" {
		t.Errorf("expected the nearest effort above the one wanted, got %q", offers[0].Effort)
	}
	if !slices.Equal(offers[0].EffortLevels, []string{"low", "medium", "xhigh"}) {
		t.Errorf("expected the efforts in order, got %v", offers[0].EffortLevels)
	}
	if offers[1].Effort != "none" {
		t.Errorf("expected the only effort there is, got %q", offers[1].Effort)
	}
	if offers[2].Effort != "whatever" {
		t.Errorf("expected an unrecognised effort to be offered as it stands, got %q", offers[2].Effort)
	}
	if offers[0].Name != "Codex 5.3" || offers[0].ID != "gpt-5.3-codex" {
		t.Errorf("expected the model to be named for a person, got %q of %q", offers[0].Name, offers[0].ID)
	}
	if offers[0].Provider != "Codex" || offers[0].ProviderID != "codex" {
		t.Errorf("expected the provider named for a person, got %q of %q", offers[0].Provider, offers[0].ProviderID)
	}
}

func TestThePickerOnlyOpensWhereItCanBeDrawn(t *testing.T) {
	_, err := ChooseWhenNoneSelected(ErrNoSelection, t.TempDir()+"/models.json", everyoneSignedIn, nil, nil)

	if !errors.Is(err, ErrNoSelection) {
		t.Errorf("expected the reason nothing was selected to stand, got %v", err)
	}
}

func TestOnlyAnUnselectedModelOpensThePicker(t *testing.T) {
	wanted := errors.New("something else went wrong")

	_, err := ChooseWhenNoneSelected(wanted, t.TempDir()+"/models.json", everyoneSignedIn, os.Stdin, os.Stdout)

	if !errors.Is(err, wanted) {
		t.Errorf("expected the original reason back, got %v", err)
	}
}

func TestNothingCanBeChosenWhenNoModelsAreKnown(t *testing.T) {
	if _, err := Choose(t.TempDir()+"/models.json", everyoneSignedIn, nil, nil); err == nil {
		t.Error("expected the empty model list to be refused")
	}
}

func TestThePickerRefusesToOpenWhereNoProviderIsSignedIntoAtAll(t *testing.T) {
	path := writeChoosableModels(t)

	_, err := Choose(path, func(string) bool { return false }, nil, nil)
	if !errors.Is(err, ErrNotLoggedIn) {
		t.Errorf("expected the picker to advise signing in, got %v", err)
	}
}

func TestOnlyTheModelsOfASignedIntoProviderAreOffered(t *testing.T) {
	choices := signedInto([]Choice{
		{Provider: CodexProvider, ID: "gpt-5.6-sol"},
		{Provider: AnthropicProvider, ID: "claude-opus-5"},
		{Provider: OllamaProvider, ID: "qwen3.8:27b"},
	}, func(providerName string) bool { return providerName != AnthropicProvider })

	offeredNames := make([]string, len(choices))
	for i, choice := range choices {
		offeredNames[i] = choice.Provider + "/" + choice.ID
	}

	want := []string{"codex/gpt-5.6-sol", "ollama/qwen3.8:27b"}
	if !slices.Equal(offeredNames, want) {
		t.Errorf("got %v, want %v", offeredNames, want)
	}
}

func everyoneSignedIn(string) bool {
	return true
}

func writeChoosableModels(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "models.json")
	cache := modelCache{
		Version:   cacheVersion,
		CheckedAt: time.Now(),
		Providers: map[string]cachedModels{
			CodexProvider: {Models: []agent.Model{
				{ID: "gpt-5.6-sol", EffortLevels: []string{"high"}, MaxOutputTokens: 128_000},
			}},
		},
	}
	if err := saveModelCache(path, cache); err != nil {
		t.Fatal(err)
	}

	return path
}
