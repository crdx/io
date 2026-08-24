package main

import (
	"os"
	"path/filepath"
	"testing"

	"crdx.org/io/cmd/oh/model"
)

func testModelSelections() []model.Selection {
	return []model.Selection{
		{Provider: opencodeGoProvider, Model: "deepseek-v4-pro", Effort: "high"},
		{Provider: anthropicProvider, Model: "claude-opus-5", Effort: "max"},
	}
}

func useRoundRobinModelCache(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	path := modelCachePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}

	data := []byte(`{"version":1,"providers":{"codex":{"models":[{"id":"gpt-5.6-sol","efforts":["none","high"],"output":128000}]},"opencode-go":{"models":[{"id":"deepseek-v4-pro","efforts":["high","max"],"output":128000}]},"anthropic":{"models":[{"id":"claude-opus-5","efforts":["high","max"],"output":128000}]}}}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}
