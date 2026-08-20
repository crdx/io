package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/io/provider/chat"
)

func TestLoginOpenCodePromptsAndStoresTheKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	var output bytes.Buffer
	if err := loginOpenCodeGo(strings.NewReader("  pasted-key  \n"), &output, path); err != nil {
		t.Fatal(err)
	}

	if output.String() != "OpenCode Go API key: " {
		t.Errorf("got prompt %q", output.String())
	}
	key, err := chat.StoredKeyAt(path)
	if err != nil {
		t.Fatal(err)
	}
	if key != "pasted-key" {
		t.Errorf("got key %q", key)
	}
}

func TestLoginOpenCodeRejectsAnEmptyKey(t *testing.T) {
	err := loginOpenCodeGo(strings.NewReader(" \n"), &bytes.Buffer{}, filepath.Join(t.TempDir(), "auth.json"))
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("got error %v", err)
	}
}
