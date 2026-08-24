package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/io/cmd/oh/model"

	"crdx.org/io/cmd/oh/store"
)

func TestResolveProviderChoiceFallsBackToTheConfig(t *testing.T) {
	configured := []model.Selection{{
		Provider: opencodeGoProvider,
		Model:    "configured-model",
		Effort:   "medium",
	}}
	providerName, model, effort, err := resolveProviderChoice(
		"", "", "", configured, filepath.Join(t.TempDir(), "round-robin.json"), nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if providerName != opencodeGoProvider || model != "configured-model" || effort != "medium" {
		t.Errorf("got provider %q, model %q, and effort %q", providerName, model, effort)
	}
}

func TestResolveProviderChoicePrefersTheCommandLine(t *testing.T) {
	resumed := &store.Session{Meta: store.Meta{
		Provider: opencodeGoProvider,
		Model:    "saved-model",
		Effort:   "low",
	}}
	providerName, model, effort, err := resolveProviderChoice(
		opencodeGoProvider,
		"requested-model",
		"high",
		[]model.Selection{{Provider: codexProvider, Model: "configured-model", Effort: "medium"}},
		"",
		resumed,
	)
	if err != nil {
		t.Fatal(err)
	}
	if providerName != opencodeGoProvider || model != "requested-model" || effort != "high" {
		t.Errorf("got provider %q, model %q, and effort %q", providerName, model, effort)
	}
}

func TestResolveProviderChoiceRefusesToResumeUnderAnotherProvider(t *testing.T) {
	resumed := &store.Session{Meta: store.Meta{Provider: opencodeGoProvider, Model: "saved-model"}}

	providerName, model, effort, err := resolveProviderChoice(
		codexProvider, "requested-model", "high", nil, "", resumed,
	)
	if err == nil || !strings.Contains(err.Error(), "cannot resume a opencode-go session with codex") {
		t.Fatalf("got provider %q, model %q, effort %q, and error %v", providerName, model, effort, err)
	}
}

func TestResolveProviderChoiceResumesUnderTheRecordedProvider(t *testing.T) {
	resumed := &store.Session{Meta: store.Meta{Provider: opencodeGoProvider, Model: "saved-model", Effort: "low"}}

	providerName, model, effort, err := resolveProviderChoice(
		"", "", "",
		[]model.Selection{{Provider: codexProvider, Model: "configured-model", Effort: "medium"}},
		"",
		resumed,
	)
	if err != nil {
		t.Fatal(err)
	}
	if providerName != opencodeGoProvider || model != "saved-model" || effort != "low" {
		t.Errorf("got provider %q, model %q, and effort %q", providerName, model, effort)
	}
}

func TestResolveProviderChoiceRequiresAModel(t *testing.T) {
	providerName, model, effort, err := resolveProviderChoice("", "", "", nil, "", nil)
	if err == nil || !strings.Contains(err.Error(), "-m provider/model@effort") {
		t.Fatalf("got provider %q, model %q, effort %q, and error %v", providerName, model, effort, err)
	}
}

func TestAnExplicitModelDoesNotAdvanceTheConfiguredRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "round-robin.json")
	providerName, model, effort, err := resolveProviderChoice(
		codexProvider, "requested", "high", testModelSelections(), path, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if providerName != codexProvider || model != "requested" || effort != "high" {
		t.Errorf("got %s/%s@%s", providerName, model, effort)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected no cursor state, got %v", err)
	}
}

func TestAResumedModelDoesNotAdvanceTheConfiguredRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "round-robin.json")
	resumed := &store.Session{Meta: store.Meta{Provider: codexProvider, Model: "saved", Effort: "high"}}
	providerName, model, effort, err := resolveProviderChoice("", "", "", testModelSelections(), path, resumed)
	if err != nil {
		t.Fatal(err)
	}
	if providerName != codexProvider || model != "saved" || effort != "high" {
		t.Errorf("got %s/%s@%s", providerName, model, effort)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected no cursor state, got %v", err)
	}
}

func TestAnthropicConnectsBeforeItNeedsCredentials(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	client, err := connect(model.Choice{Provider: anthropicProvider, Model: "claude-opus-5", MaxOutputTokens: 128_000}, "high", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("expected a connection")
	}
}

func TestConnectReportsWhatTheProviderRefused(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	tests := []struct {
		name     string
		choice   model.Choice
		endpoint string
		want     string
	}{
		{
			"codex",
			model.Choice{Provider: codexProvider},
			"",
			"codex: Model is empty",
		},
		{
			"opencode-go",
			model.Choice{Provider: opencodeGoProvider, Model: "deepseek-v4-pro"},
			"http://somewhere",
			"chat: MaxOutputTokens is 0",
		},
		{
			"anthropic",
			model.Choice{Provider: anthropicProvider, MaxOutputTokens: 128_000},
			"",
			"anthropic: Model is empty",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, err := connect(test.choice, "high", test.endpoint)

			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q, got %v", test.want, err)
			}

			if client != nil {
				t.Errorf("expected no connection to be handed back, got %+v", client)
			}
		})
	}
}

func TestOpenCodeRequiresLogin(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	_, err := connect(model.Choice{Provider: opencodeGoProvider, Model: "deepseek-v4-pro", MaxOutputTokens: 128_000}, "high", "")
	if err == nil || !strings.Contains(err.Error(), "login command with opencode-go") {
		t.Fatalf("got error %v", err)
	}
}
