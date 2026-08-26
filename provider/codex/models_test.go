package codex

import "testing"

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
