package codex

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Credentials are what a ChatGPT subscription was authorised as.
type Credentials struct {
	Access    string `json:"access"`
	Refresh   string `json:"refresh"`
	ExpiresAt int64  `json:"expires_at"`
	AccountID string `json:"account_id"`
}

func (self *Credentials) stale() bool {
	return time.Now().Add(refreshWindow).UnixMilli() >= self.ExpiresAt
}

// CredentialsPath is where Login writes and Auth reads.
func CredentialsPath() string {
	base := os.Getenv("XDG_STATE_HOME")

	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}

		base = filepath.Join(home, ".local", "state")
	}

	return filepath.Join(base, "org.crdx", "io", "credentials.json")
}

func loadCredentials(path string) (*Credentials, error) {
	if path == "" {
		return nil, errors.New("could not determine where credentials live")
	}

	data, err := os.ReadFile(path) //nolint:gosec // the path is ours, not user input
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errors.New("not logged in: run the login command")
		}

		return nil, fmt.Errorf("read credentials: %w", err)
	}

	var credentials Credentials
	if err := json.Unmarshal(data, &credentials); err != nil {
		return nil, fmt.Errorf("parse credentials %s: %w", path, err)
	}

	return &credentials, nil
}

func saveCredentials(path string, credentials *Credentials) error {
	if path == "" {
		return errors.New("could not determine where credentials live")
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}

	data, err := json.Marshal(credentials)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o600)
}
