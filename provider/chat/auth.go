package chat

import (
	"errors"
	"os"

	"crdx.org/io/internal/auth"
)

// CredentialsPath is where login writes and clients read the OpenCode Go API key.
func CredentialsPath() string {
	return auth.Path()
}

// SaveKey stores an OpenCode Go API key without replacing other provider credentials.
func SaveKey(key string) error {
	return SaveKeyAt(CredentialsPath(), key)
}

// SaveKeyAt stores an OpenCode Go API key at path.
func SaveKeyAt(path string, key string) error {
	return auth.SaveOpenCodeGoKey(path, key)
}

// StoredKey reads the OpenCode Go API key saved by the login command.
func StoredKey() (string, error) {
	return StoredKeyAt(CredentialsPath())
}

// StoredKeyAt reads an OpenCode Go API key from path.
func StoredKeyAt(path string) (string, error) {
	credentials, err := auth.Load(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", errors.New("not logged in to OpenCode Go: run the login command with opencode-go")
	}
	if err != nil {
		return "", err
	}
	if credentials.OpenCodeGo == nil || credentials.OpenCodeGo.APIKey == "" {
		return "", errors.New("not logged in to OpenCode Go: run the login command with opencode-go")
	}

	return credentials.OpenCodeGo.APIKey, nil
}
