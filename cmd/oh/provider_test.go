package main

import (
	"strings"
	"testing"

	"crdx.org/io/cmd/oh/store"
)

func TestResolveProviderSettingsFallsBackToConfiguration(t *testing.T) {
	providerName, model, effort, err := resolveProviderSettings("", "", "", configuredSettings{
		Provider: opencodeGoProvider,
		Model:    "configured-model",
		Effort:   "medium",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if providerName != opencodeGoProvider || model != "configured-model" || effort != "medium" {
		t.Errorf("got provider %q, model %q, and effort %q", providerName, model, effort)
	}
}

func TestResolveProviderSettingsPrefersTheCommandLine(t *testing.T) {
	resumed := &store.Session{Meta: store.Meta{
		Provider: opencodeGoProvider,
		Model:    "saved-model",
		Effort:   "low",
	}}
	providerName, model, effort, err := resolveProviderSettings(
		opencodeGoProvider,
		"requested-model",
		"high",
		configuredSettings{
			Provider: opencodeGoProvider,
			Model:    "configured-model",
			Effort:   "medium",
		},
		resumed,
	)
	if err != nil {
		t.Fatal(err)
	}
	if providerName != opencodeGoProvider || model != "requested-model" || effort != "high" {
		t.Errorf("got provider %q, model %q, and effort %q", providerName, model, effort)
	}
}

func TestResolveProviderSettingsRefusesToResumeUnderAnotherProvider(t *testing.T) {
	resumed := &store.Session{Meta: store.Meta{Provider: opencodeGoProvider, Model: "saved-model"}}

	providerName, model, effort, err := resolveProviderSettings(codexProvider, "requested-model", "high", configuredSettings{}, resumed)
	if err == nil || !strings.Contains(err.Error(), "cannot resume a opencode-go session with codex") {
		t.Fatalf("got provider %q, model %q, effort %q, and error %v", providerName, model, effort, err)
	}
}

func TestResolveProviderSettingsResumesUnderTheRecordedProvider(t *testing.T) {
	resumed := &store.Session{Meta: store.Meta{Provider: opencodeGoProvider, Model: "saved-model", Effort: "low"}}

	providerName, model, effort, err := resolveProviderSettings("", "", "", configuredSettings{
		Provider: codexProvider,
		Model:    "configured-model",
	}, resumed)
	if err != nil {
		t.Fatal(err)
	}
	if providerName != opencodeGoProvider || model != "saved-model" || effort != "low" {
		t.Errorf("got provider %q, model %q, and effort %q", providerName, model, effort)
	}
}

func TestResolveProviderSettingsRequiresAModel(t *testing.T) {
	providerName, model, effort, err := resolveProviderSettings("", "", "", configuredSettings{}, nil)
	if err == nil || !strings.Contains(err.Error(), "-m provider/model@effort") {
		t.Fatalf("got provider %q, model %q, effort %q, and error %v", providerName, model, effort, err)
	}
}

func TestOpenCodeRequiresLogin(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	_, err := connect(opencodeGoProvider, "deepseek-v4-pro", "high", "")
	if err == nil || !strings.Contains(err.Error(), "login command with opencode-go") {
		t.Fatalf("got error %v", err)
	}
}
