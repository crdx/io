package main

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/io/internal/sim"
)

func buildTestBinary(t *testing.T) string {
	t.Helper()

	binary := filepath.Join(t.TempDir(), "oh")
	command := exec.CommandContext(t.Context(), "go", "build", "-o", binary, ".") //nolint:gosec // fixed Go tool and test output path
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build oh: %v\n%s", err, output)
	}
	return binary
}

func runTestBinary(t *testing.T, binary string, environment []string, arguments ...string) string {
	t.Helper()

	command := exec.CommandContext(t.Context(), binary, arguments...) //nolint:gosec // binary was built by this test
	command.Env = environment
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("oh %s: %v\n%s", strings.Join(arguments, " "), err, output)
	}
	return string(output)
}

func testBinaryEnvironment(t *testing.T, stateDirectory string) []string {
	t.Helper()

	return append(os.Environ(),
		"HOME="+t.TempDir(),
		"XDG_CONFIG_HOME="+t.TempDir(),
		"XDG_STATE_HOME="+stateDirectory,
	)
}

func TestModelListDispatchRunsThroughTheBinary(t *testing.T) {
	binary := buildTestBinary(t)
	stateDirectory := t.TempDir()
	cachePath := filepath.Join(stateDirectory, namespace, app, "models.json")
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o700); err != nil {
		t.Fatal(err)
	}
	cache := []byte(`{"version":1,"providers":{"codex":{"models":[{"id":"gpt-cli","efforts":["high"],"output":128000}]}}}`)
	if err := os.WriteFile(cachePath, cache, 0o600); err != nil {
		t.Fatal(err)
	}

	output := runTestBinary(t, binary, testBinaryEnvironment(t, stateDirectory), "-l")
	if output != "codex/gpt-cli\n" {
		t.Errorf("got %q", output)
	}
}

func TestModelUpdateDispatchRunsThroughTheBinary(t *testing.T) {
	binary := buildTestBinary(t)
	endpoint := sim.New(&sim.Scenario{Model: "fake", Turns: []sim.Turn{{Say: "Hello."}}})
	server := httptest.NewServer(endpoint)
	t.Cleanup(server.Close)

	address := endpoint.Addresses(server.URL)[sim.Messages]
	stateDirectory := t.TempDir()
	environment := append(testBinaryEnvironment(t, stateDirectory), endpointVariable+"="+address)

	updated := runTestBinary(t, binary, environment, "-u")
	if !strings.Contains(updated, "Stored model list") {
		t.Errorf("update output did not report storage: %q", updated)
	}

	listed := runTestBinary(t, binary, environment, "-l")
	for _, providerName := range providerNames {
		if !strings.Contains(listed, providerName+"/fake") {
			t.Errorf("listing omitted %s: %q", providerName, listed)
		}
	}
}
