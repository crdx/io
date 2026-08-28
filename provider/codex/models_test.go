package codex

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestModelsLeavesUnlistedCapabilitiesUnknown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(writer, `{"models":[{"slug":"first","title":"First"}],"data":[{"id":"second"}]}`)
	}))
	t.Cleanup(server.Close)

	client, err := New(Static("token", "account"), "first", "high")
	if err != nil {
		t.Fatal(err)
	}
	client.URL = server.URL + "/codex/responses"

	models, err := client.Models(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].ID != "first" || models[0].Name != "First" || models[1].ID != "second" {
		t.Fatalf("got models %+v", models)
	}
	if models[0].EffortLevels != nil || models[1].EffortLevels != nil {
		t.Errorf("the listing claimed effort levels it did not report: %+v", models)
	}
}

func TestOnlyResponsesModelsAreSupported(t *testing.T) {
	for _, modelID := range []string{
		"gpt-audio-2",
		"text-embedding-4",
		"gpt-image-2",
		"omni-moderation-2",
		"gpt-realtime-2.1",
		"gpt-transcribe-2",
		"gpt-tts-2",
	} {
		if SupportsResponses(modelID) {
			t.Errorf("expected %s not to support Responses", modelID)
		}
	}

	for _, modelID := range []string{"gpt-5.6-sol", "gpt-5.3-codex", "o3"} {
		if !SupportsResponses(modelID) {
			t.Errorf("expected %s to support Responses", modelID)
		}
	}
}
