package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/io/cmd/oh/cli"
)

func TestCompletionProtocolMatchesTheGolden(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	cachePath := modelCachePath()
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o700); err != nil {
		t.Fatal(err)
	}
	cache := []byte(`{"version":1,"providers":{"codex":{"models":[{"id":"gpt-5","efforts":["low","high"],"output":128000}]},"anthropic":{"models":[{"id":"claude-sonnet-5","efforts":["none","high"],"output":128000}]}}}`)
	if err := os.WriteFile(cachePath, cache, 0o600); err != nil {
		t.Fatal(err)
	}

	directory := t.TempDir()
	writeStoredSession(t, directory, "older-badger", "2024-01-01T00:00:00Z")
	writeStoredSession(t, directory, "newer-jaguar", "2025-01-01T00:00:00Z")
	sources := cli.Sources{ModelCachePath: cachePath, SessionsDir: directory}

	requests := []struct {
		name string
		args []string
	}{
		{name: "options", args: []string{"--complete", "option", ""}},
		{name: "models", args: []string{"--complete", "model", "sonnet"}},
		{name: "efforts", args: []string{"--complete", "effort", "sonnet@"}},
		{name: "capabilities", args: []string{"--complete", "caps", "rxw"}},
		{name: "sessions", args: []string{"--complete", "session", ""}},
	}

	var output strings.Builder
	for _, request := range requests {
		completions, wanted := cli.Complete(request.args, sources)
		if !wanted {
			t.Fatalf("%s completion request was not recognised", request.name)
		}
		_, _ = output.WriteString("=== " + request.name + " ===\n")
		for _, completion := range completions {
			_, _ = output.WriteString(completion + "\n")
		}
	}

	goldenPath := filepath.Join("testdata", "output", "completion.txt")
	if *updateGoldens {
		if err := os.WriteFile(goldenPath, []byte(output.String()), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath) //nolint:gosec // fixed testdata path
	if err != nil {
		t.Fatal(err)
	}
	if output.String() != string(want) {
		t.Errorf("completion protocol differs from %s\n--- got ---\n%s--- want ---\n%s", goldenPath, output.String(), want)
	}
}
