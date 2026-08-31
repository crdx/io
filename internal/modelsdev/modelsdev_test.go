package modelsdev

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchOmitsModelsWithDatedVersionSuffixes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"anthropic": {"models": {
				"claude-opus-4-5": {},
				"claude-opus-4-5-20251101": {},
				"dated-id-under-an-alias": {"id": "claude-sonnet-4-5-20250929"},
				"not-a-date": {"id": "claude-release-99999999"}
			}},
			"openai": {"models": {
				"gpt-snapshot-20250101": {}
			}}
		}`))
	}))
	defer server.Close()

	registry, err := Fetch(t.Context(), server.URL, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	anthropic := registry.Provider("anthropic")
	if len(anthropic) != 2 {
		t.Fatalf("expected only undated Anthropic models, got %v", anthropic)
	}
	if _, isFound := anthropic["claude-opus-4-5"]; !isFound {
		t.Error("expected the undated model to remain")
	}
	if _, isFound := anthropic["not-a-date"]; !isFound {
		t.Error("expected a non-date numeric suffix to remain")
	}
	if len(registry.Provider("openai")) != 0 {
		t.Errorf("expected dated models from every provider to be omitted, got %v", registry.Provider("openai"))
	}
}

func TestFetchTakesTheContextWindowFromTheInputShareOfTheBudget(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"openai": {"models": {
				"split-budget": {"limit": {"context": 1050000, "input": 922000, "output": 128000}},
				"whole-budget": {"limit": {"context": 400000, "output": 128000}}
			}}
		}`))
	}))
	defer server.Close()

	registry, err := Fetch(t.Context(), server.URL, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	openai := registry.Provider("openai")
	if got := openai["split-budget"].ContextWindowTokens; got != 922_000 {
		t.Errorf("got %d, want the input share 922000", got)
	}
	if got := openai["whole-budget"].ContextWindowTokens; got != 400_000 {
		t.Errorf("got %d, want the whole budget 400000", got)
	}
	if got := openai["split-budget"].MaxOutputTokens; got != 128_000 {
		t.Errorf("got %d, want the output limit 128000", got)
	}
}
