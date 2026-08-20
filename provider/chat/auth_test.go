package chat_test

import (
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/io/internal/auth"
	"crdx.org/io/provider/chat"
)

func TestStoredKeyReadsOpenCodeGoCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := auth.SaveOpenCodeGoKey(path, "stored-key"); err != nil {
		t.Fatal(err)
	}

	key, err := chat.StoredKeyAt(path)
	if err != nil {
		t.Fatal(err)
	}
	if key != "stored-key" {
		t.Errorf("got key %q", key)
	}
}

func TestStoredKeyReportsMissingOpenCodeGoCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := auth.Save(path, &auth.Credentials{Codex: &auth.CodexCredentials{Access: "codex-access"}}); err != nil {
		t.Fatal(err)
	}

	_, err := chat.StoredKeyAt(path)
	if err == nil || !strings.Contains(err.Error(), "login command with opencode-go") {
		t.Fatalf("got error %v", err)
	}
}
