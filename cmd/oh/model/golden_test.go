package model

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/io/agent"
)

const (
	goldenCachePath       = "/state/models.json"
	registryPrefix        = "models.dev: "
	goldenRegistryFailure = `Get "<registry>": connect: connection refused`
)

var updateGoldens = flag.Bool("update", false, "write what was drawn back to the golden files")

func TestAnUpdateWithNothingReachableMatchesTheGolden(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	var output bytes.Buffer
	err := Update(&output, deadAddress, modelCachePath(), unreachableProviders)
	if err == nil {
		t.Fatal("expected an update with nothing reachable to fail")
	}

	assertGolden(t, "update-nothing-reachable.txt", strings.Join([]string{
		report(t, output.String()),
		"=== error ===\n", err.Error(), "\n",
	}, ""))
}

func TestAnUpdateFromTheRegistryAloneMatchesTheGolden(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	var output bytes.Buffer
	if err := Update(&output, serveRegistry(t, oneCodexModel), modelCachePath(), unreachableProviders); err != nil {
		t.Fatal(err)
	}

	assertGolden(t, "update-from-registry.txt", report(t, output.String()))
}

func TestAnUpdateAProviderListsItselfMatchesTheGolden(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	listed := []agent.Model{
		{ID: "gpt-5.6-sol", Name: "GPT-5.6 Sol", EffortLevels: []string{"low", "high"}},
		{ID: "an-unselectable-model"},
	}

	var output bytes.Buffer
	lister := func(ctx context.Context, providerName string) ([]agent.Model, error) {
		if providerName == OllamaProvider {
			return listed, nil
		}

		return unreachableProviders(ctx, providerName)
	}
	if err := Update(&output, serveRegistry(t, oneCodexModel), modelCachePath(), lister); err != nil {
		t.Fatal(err)
	}

	assertGolden(t, "update-listed-by-provider.txt", report(t, output.String()))
}

func unreachableProviders(_ context.Context, providerName string) ([]agent.Model, error) {
	switch providerName {
	case CodexProvider:
		return nil, agent.ErrNoListing
	case OpencodeGoProvider:
		return nil, errors.New("not logged in to OpenCode Go: run the login command with opencode-go")
	case AnthropicProvider:
		return nil, errors.New("not logged in to Anthropic: run the login command with anthropic")
	default:
		return nil, errors.New(`Get "http://localhost:11434/api/tags": connect: connection refused`)
	}
}

func report(t *testing.T, drawn string) string {
	t.Helper()

	drawn = strings.ReplaceAll(drawn, modelCachePath(), goldenCachePath)
	lines := strings.Split(drawn, "\n")
	for index, line := range lines {
		if strings.HasPrefix(line, registryPrefix) {
			lines[index] = registryPrefix + goldenRegistryFailure
		}
	}

	return strings.Join(lines, "\n")
}

func assertGolden(t *testing.T, name string, drawn string) {
	t.Helper()

	goldenPath := filepath.Join("testdata", name)
	if *updateGoldens {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, []byte(drawn), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath) //nolint:gosec // fixed testdata path
	if err != nil {
		t.Fatal(err)
	}
	if drawn != string(want) {
		t.Errorf("output differs from %s\n--- got ---\n%s--- want ---\n%s", goldenPath, drawn, want)
	}
}
