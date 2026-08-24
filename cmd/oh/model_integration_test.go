package main

import (
	"bytes"
	"net/http/httptest"
	"testing"

	"crdx.org/io/cmd/oh/config"
	"crdx.org/io/cmd/oh/model"
	"crdx.org/io/internal/sim"
)

func TestAnEffortWrittenAsAnAliasInTheConfigIsResolved(t *testing.T) {
	_, _, effort, err := resolveProviderChoice(
		"", "", "",
		config.Config{Provider: codexProvider, Model: "gpt-5.6-sol", Effort: "off"},
		nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if effort != "none" {
		t.Errorf("expected the level rather than the word that asked for it, got %q", effort)
	}
}

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
	if err := model.Update(&output, address, modelCachePath(), listProviderModels); err != nil {
		t.Fatalf("unexpected error: %v, output %q", err, output.String())
	}

	choices := model.Choices(modelCachePath())
	for _, providerName := range providerNames {
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
