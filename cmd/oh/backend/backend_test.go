package backend

import (
	"bytes"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/io/internal/sim"

	"crdx.org/io/cmd/oh/location"
	"crdx.org/io/cmd/oh/model"
)

const (
	codexProvider      = model.CodexProvider
	opencodeGoProvider = model.OpencodeGoProvider
	anthropicProvider  = model.AnthropicProvider
)

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
	if err := model.Update(&output, address, location.GetModelCachePath(os.Getenv(EndpointVariable) != ""), ListModels); err != nil {
		t.Fatalf("unexpected error: %v, output %q", err, output.String())
	}

	choices := model.Choices(location.GetModelCachePath(os.Getenv(EndpointVariable) != ""))
	for _, providerName := range model.ProviderNames() {
		var matches []model.Choice
		for _, choice := range choices {
			if choice.Provider == providerName {
				matches = append(matches, choice)
			}
		}

		if len(matches) != 1 || matches[0].Model != "fake" {
			t.Errorf("expected %s to offer the scenario's model, got %v", providerName, matches)

			continue
		}

		if matches[0].MaxOutputTokens <= 0 {
			t.Errorf("expected %s to know what the model may write, got %v", providerName, matches[0])
		}
	}
}

func TestEveryProviderListsModelsWithoutAConversationModel(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	endpoint := sim.New(&sim.Scenario{Model: "fake"})
	server := httptest.NewServer(endpoint)
	t.Cleanup(server.Close)

	addresses := endpoint.Addresses(server.URL)
	tests := []struct {
		providerName string
		format       string
	}{
		{codexProvider, sim.Responses},
		{opencodeGoProvider, sim.Completions},
		{anthropicProvider, sim.Messages},
	}

	for _, test := range tests {
		t.Run(test.providerName, func(t *testing.T) {
			models, err := ListModels(t.Context(), test.providerName, addresses[test.format])
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(models) != 1 || models[0].ID != "fake" {
				t.Errorf("got %v", models)
			}
		})
	}
}

func TestSubscriptionCodexDoesNotTrustTheUndocumentedModelListing(t *testing.T) {
	models, err := ListModels(t.Context(), codexProvider, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) != 0 {
		t.Errorf("got %v", models)
	}
}

func testModelSelections() []model.Selection {
	return []model.Selection{
		{Provider: opencodeGoProvider, Model: "deepseek-v4-pro", Effort: "high"},
		{Provider: anthropicProvider, Model: "claude-opus-5", Effort: "max"},
	}
}

func TestResolveFallsBackToTheConfig(t *testing.T) {
	configured := []model.Selection{{
		Provider: opencodeGoProvider,
		Model:    "configured-model",
		Effort:   "medium",
	}}
	selection, err := Resolve(
		model.Selection{}, model.Selection{}, configured, filepath.Join(t.TempDir(), "round-robin.json"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if selection != configured[0] {
		t.Errorf("got %s", selection)
	}
}

func TestResolvePrefersTheCommandLine(t *testing.T) {
	resumed := model.Selection{
		Provider: opencodeGoProvider,
		Model:    "saved-model",
		Effort:   "low",
	}
	requested := model.Selection{
		Provider: opencodeGoProvider,
		Model:    "requested-model",
		Effort:   "high",
	}
	selection, err := Resolve(
		requested,
		resumed,
		[]model.Selection{{Provider: codexProvider, Model: "configured-model", Effort: "medium"}},
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	if selection != requested {
		t.Errorf("got %s", selection)
	}
}

func TestResolveRefusesToResumeUnderAnotherProvider(t *testing.T) {
	resumed := model.Selection{Provider: opencodeGoProvider, Model: "saved-model"}

	selection, err := Resolve(
		model.Selection{Provider: codexProvider, Model: "requested-model", Effort: "high"},
		resumed,
		nil,
		"",
	)
	if err == nil || !strings.Contains(err.Error(), "cannot resume a opencode-go session with codex") {
		t.Fatalf("got selection %s and error %v", selection, err)
	}
}

func TestResolveResumesUnderTheRecordedProvider(t *testing.T) {
	resumed := model.Selection{Provider: opencodeGoProvider, Model: "saved-model", Effort: "low"}

	selection, err := Resolve(
		model.Selection{},
		resumed,
		[]model.Selection{{Provider: codexProvider, Model: "configured-model", Effort: "medium"}},
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	if selection != resumed {
		t.Errorf("got %s", selection)
	}
}

func TestResolveRequiresAModel(t *testing.T) {
	selection, err := Resolve(model.Selection{}, model.Selection{}, nil, "")
	if err == nil || !strings.Contains(err.Error(), "-m provider/model@effort") {
		t.Fatalf("got selection %s and error %v", selection, err)
	}
}

func TestAnExplicitModelDoesNotAdvanceTheConfiguredRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "round-robin.json")
	requested := model.Selection{Provider: codexProvider, Model: "requested", Effort: "high"}
	selection, err := Resolve(requested, model.Selection{}, testModelSelections(), path)
	if err != nil {
		t.Fatal(err)
	}
	if selection != requested {
		t.Errorf("got %s", selection)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected no cursor state, got %v", err)
	}
}

func TestAResumedModelDoesNotAdvanceTheConfiguredRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "round-robin.json")
	resumed := model.Selection{Provider: codexProvider, Model: "saved", Effort: "high"}
	selection, err := Resolve(model.Selection{}, resumed, testModelSelections(), path)
	if err != nil {
		t.Fatal(err)
	}
	if selection != resumed {
		t.Errorf("got %s", selection)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected no cursor state, got %v", err)
	}
}

func TestAnthropicConnectsBeforeItNeedsCredentials(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	client, err := Connect(model.Choice{Provider: anthropicProvider, Model: "claude-opus-5", MaxOutputTokens: 128_000}, "high", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("expected a connection")
	}
}

func TestEveryConnectionCarriesAWebSearchClient(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	endpoint := sim.New(&sim.Scenario{Model: "fake", Turns: []sim.Turn{{Say: "Hello."}}})
	server := httptest.NewServer(endpoint)
	t.Cleanup(server.Close)

	address := endpoint.Addresses(server.URL)[sim.Messages]

	for _, providerName := range []string{codexProvider, opencodeGoProvider, anthropicProvider} {
		client, err := Connect(
			model.Choice{Provider: providerName, Model: "fake", MaxOutputTokens: 128_000},
			"high",
			address,
		)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", providerName, err)
		}
		if client.Search == nil {
			t.Fatalf("%s: connection carries no search client", providerName)
		}
		if client.Search.URL != address {
			t.Errorf("%s: search asks %q, want %q", providerName, client.Search.URL, address)
		}
		if client.Search.Model != webSearchModel {
			t.Errorf("%s: search asks %q, want %q", providerName, client.Search.Model, webSearchModel)
		}
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
			client, err := Connect(test.choice, "high", test.endpoint)

			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q, got %v", test.want, err)
			}

			if client != nil {
				t.Errorf("expected no connection to be handed back, got %+v", client)
			}
		})
	}
}

func TestConnectRefusesAnUnknownProvider(t *testing.T) {
	client, err := Connect(model.Choice{Provider: "nowhere"}, "high", "")
	if err == nil || !strings.Contains(err.Error(), `unknown provider "nowhere"`) {
		t.Fatalf("got connection %+v and error %v", client, err)
	}
}

func TestOpenCodeRequiresLogin(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	_, err := Connect(model.Choice{Provider: opencodeGoProvider, Model: "deepseek-v4-pro", MaxOutputTokens: 128_000}, "high", "")
	if err == nil || !strings.Contains(err.Error(), "login command with opencode-go") {
		t.Fatalf("got error %v", err)
	}
}
