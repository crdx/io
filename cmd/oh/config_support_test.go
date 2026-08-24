package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/io/cmd/oh/config"
)

func configFrom(t *testing.T, body string) config.Config {
	t.Helper()

	if body == "" {
		config, err := config.Load("")
		if err != nil {
			t.Fatal(err)
		}

		return config
	}

	path := filepath.Join(t.TempDir(), "config.toml")
	version := fmt.Sprintf("version = %d\n", config.Format)
	if err := os.WriteFile(path, []byte(version+undentConfig(body)), 0o600); err != nil {
		t.Fatal(err)
	}

	config, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	return config
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

	config, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}

	return config
}
