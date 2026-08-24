package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/io/cmd/oh/config"
)

func configFrom(t *testing.T, body string) config.Config {
	t.Helper()

	if body == "" {
		settings, err := config.Load("")
		if err != nil {
			t.Fatal(err)
		}

		return settings
	}

	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(undentConfig(body)), 0o600); err != nil {
		t.Fatal(err)
	}

	settings, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	return settings
}

func undentConfig(body string) string {
	var output strings.Builder
	for row := range strings.SplitSeq(strings.TrimSpace(body), "\n") {
		output.WriteString(strings.TrimLeft(row, "\t"))
		output.WriteString("\n")
	}

	return output.String()
}

func builtInConfig(t *testing.T) config.Config {
	t.Helper()

	settings, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}

	return settings
}
