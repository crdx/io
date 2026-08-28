package codex

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"crdx.org/io/internal/req"
)

const refreshWindow = 5 * time.Minute

// Token is what one request is authorised with.
type Token struct {
	Access    string
	AccountID string
}

// TokenSource hands over a token to make a request with.
type TokenSource interface {
	Token() (Token, error)
}

type observedTokenSource interface {
	observeHTTP(req.Observer)
}

// Static is a token that is already held, and never changes.
func Static(access string, accountID string) TokenSource {
	return static{token: Token{Access: access, AccountID: accountID}}
}

type static struct {
	token Token // the token always returned
}

func (self static) Token() (Token, error) {
	return self.token, nil
}

// StoredCredentials reads and refreshes credentials written by Login.
func StoredCredentials() TokenSource {
	return &credentialStore{path: CredentialsPath(), requests: req.New(authTimeout)}
}

// StoredCredentialsAt reads credentials from path.
func StoredCredentialsAt(path string) TokenSource {
	return &credentialStore{path: path, requests: req.New(authTimeout)}
}

type credentialStore struct {
	path        string
	requests    *req.Client
	mutex       sync.Mutex // guards loading and refreshing
	credentials *Credentials
}

// Token reads the credentials on first use and refreshes them when needed.
func (self *credentialStore) Token() (Token, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if self.credentials == nil {
		credentials, err := loadCredentials(self.path)
		if err != nil {
			return Token{}, err
		}

		self.credentials = credentials
	}

	if stale(self.credentials) {
		if err := self.refresh(); err != nil {
			return Token{}, err
		}
	}

	return Token{
		Access:    self.credentials.Access,
		AccountID: self.credentials.AccountID,
	}, nil
}

func (self *credentialStore) observeHTTP(observer req.Observer) {
	self.requests.Observe(observer)
}

func (self *credentialStore) refresh() error {
	if self.credentials.Refresh == "" {
		return errors.New("credentials have expired: run the login command again")
	}

	refreshedToken, err := refreshToken(self.requests, self.credentials.Refresh)
	if err != nil {
		if req.IsRejected(err) {
			return fmt.Errorf("credentials were refused: run the login command again: %w", err)
		}

		return fmt.Errorf("refresh credentials: %w", err)
	}

	inherit(refreshedToken, self.credentials)

	self.credentials = refreshedToken

	if err := saveCredentials(self.path, refreshedToken); err != nil {
		return fmt.Errorf("store the refreshed credentials: %w", err)
	}

	return nil
}
