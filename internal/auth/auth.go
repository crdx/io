package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"crdx.org/io/internal/format"
	"crdx.org/io/internal/xdg"
)

// Version is the current credentials format.
const Version = 1

// ErrUnsupportedVersion is returned when stored credentials are in a format this build cannot read.
var ErrUnsupportedVersion = errors.New("credentials are in a format this build does not read: run the login command again")

// Unusable reports whether stored credentials could not be read and may be written over afresh.
func Unusable(err error) bool {
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, ErrUnsupportedVersion)
}

// Credentials are the provider credentials held in auth.json.
type Credentials struct {
	Version    int                    `json:"version"`
	Codex      *CodexCredentials      `json:"codex,omitempty"`
	OpenCodeGo *OpenCodeGoCredentials `json:"opencode-go,omitempty"`
	Anthropic  *AnthropicCredentials  `json:"anthropic,omitempty"`
}

// CodexCredentials are what a ChatGPT subscription was authorised as.
type CodexCredentials struct {
	Access    string `json:"access"`
	Refresh   string `json:"refresh"`
	ExpiresAt int64  `json:"expires_at"`
	AccountID string `json:"account_id"`
}

// AnthropicCredentials are what a Claude subscription was authorised as.
type AnthropicCredentials struct {
	Access    string `json:"access"`
	Refresh   string `json:"refresh"`
	ExpiresAt int64  `json:"expires_at"`
}

// OpenCodeGoCredentials authorise requests against OpenCode Go.
type OpenCodeGoCredentials struct {
	APIKey string `json:"api_key"`
}

// Path is where provider credentials are stored.
func Path() string {
	return xdg.StatePath("org.crdx", "io", "auth.json")
}

// Load reads credentials from path.
func Load(path string) (*Credentials, error) {
	if path == "" {
		return nil, errors.New("could not determine where credentials live")
	}

	data, err := os.ReadFile(path) //nolint:gosec // the path is selected by the caller
	if err != nil {
		return nil, fmt.Errorf("read credentials: %w", err)
	}

	storedVersion, err := format.ReadJSON(data)
	if err != nil {
		return nil, fmt.Errorf("parse credentials %s: %w", path, err)
	}
	if storedVersion != Version {
		return nil, fmt.Errorf("%s: format %d: %w", path, storedVersion, ErrUnsupportedVersion)
	}

	var credentials Credentials
	if err := json.Unmarshal(data, &credentials); err != nil {
		return nil, fmt.Errorf("parse credentials %s: %w", path, err)
	}

	return &credentials, nil
}

// Save writes credentials to path with user-only permissions.
func Save(path string, credentials *Credentials) error {
	if path == "" {
		return errors.New("could not determine where credentials live")
	}

	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}

	credentials.Version = Version
	data, err := json.Marshal(credentials)
	if err != nil {
		return err
	}

	pending, err := os.CreateTemp(directory, filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("write credentials: %w", err)
	}

	defer func() { _ = os.Remove(pending.Name()) }()

	if _, err := pending.Write(data); err != nil {
		_ = pending.Close()

		return fmt.Errorf("write credentials: %w", err)
	}

	if err := pending.Close(); err != nil {
		return fmt.Errorf("write credentials: %w", err)
	}

	if err := os.Rename(pending.Name(), path); err != nil {
		return fmt.Errorf("write credentials: %w", err)
	}

	return nil
}

// SaveOpenCodeGoKey updates the OpenCode Go key without replacing other provider credentials.
func SaveOpenCodeGoKey(path string, key string) error {
	credentials, err := Load(path)
	if err != nil {
		if !Unusable(err) {
			return err
		}
		credentials = &Credentials{Version: Version}
	}

	credentials.OpenCodeGo = &OpenCodeGoCredentials{APIKey: key}
	return Save(path, credentials)
}
