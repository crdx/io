package codex

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"crdx.org/io/internal/xdgutil"
)

// Credentials are what a ChatGPT subscription was authorised as.
type Credentials struct {
	Access    string `json:"access"`     // the access token
	Refresh   string `json:"refresh"`    // the refresh token
	ExpiresAt int64  `json:"expires_at"` // when access expires
	AccountID string `json:"account_id"` // the ChatGPT account
}

func (self *Credentials) stale() bool {
	return time.Now().Add(refreshWindow).UnixMilli() >= self.ExpiresAt
}

func (self *Credentials) inherit(held *Credentials) {
	if self.Refresh == "" {
		self.Refresh = held.Refresh
	}

	if self.AccountID == "" {
		self.AccountID = held.AccountID
	}
}

// CredentialsPath is where Login writes and Auth reads.
func CredentialsPath() string {
	return xdgutil.StatePath("org.crdx", "io", "auth.json")
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
