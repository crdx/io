package codex

import (
	"errors"
	"fmt"
	"os"
	"time"

	"crdx.org/io/internal/auth"
)

// Credentials are what a ChatGPT subscription was authorised as.
type Credentials = auth.CodexCredentials

func stale(credentials *Credentials) bool {
	return time.Now().Add(refreshWindow).UnixMilli() >= credentials.ExpiresAt
}

func inherit(childCredentials *Credentials, parentCredentials *Credentials) {
	if childCredentials.Refresh == "" {
		childCredentials.Refresh = parentCredentials.Refresh
	}

	if childCredentials.AccountID == "" {
		childCredentials.AccountID = parentCredentials.AccountID
	}
}

func CredentialsPath() string {
	return auth.Path()
}

func LoadStoredCredentials() (*Credentials, error) {
	return loadCredentials(CredentialsPath())
}

func loadCredentials(path string) (*Credentials, error) {
	stored, err := auth.Load(path)
	if errors.Is(err, os.ErrNotExist) || err == nil && stored.Codex == nil {
		return nil, errors.New("not logged in to ChatGPT: run the login command with codex")
	}
	if err != nil {
		return nil, err
	}

	return stored.Codex, nil
}

func saveCredentials(path string, credentials *Credentials) error {
	storedCredentials, err := auth.Load(path)
	if auth.Unusable(err) {
		storedCredentials = &auth.Credentials{Version: auth.Version}
	} else if err != nil {
		return fmt.Errorf("preserve credentials: %w", err)
	}

	if storedCredentials.Codex != nil {
		inherit(credentials, storedCredentials.Codex)
	}
	storedCredentials.Codex = credentials
	return auth.Save(path, storedCredentials)
}
