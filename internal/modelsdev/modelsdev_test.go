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
	if _, found := anthropic["claude-opus-4-5"]; !found {
		t.Error("expected the undated model to remain")
	}
	if _, found := anthropic["not-a-date"]; !found {
		t.Error("expected a non-date numeric suffix to remain")
	}
	if len(registry.Provider("openai")) != 0 {
		t.Errorf("expected dated models from every provider to be omitted, got %v", registry.Provider("openai"))
	}
}
