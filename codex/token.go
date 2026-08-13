package codex

import (
	"errors"
	"fmt"
	"sync"
	"time"
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

// Static is a token that is already held, and never changes.
func Static(access string, accountID string) TokenSource {
	return static{token: Token{Access: access, AccountID: accountID}}
}

type static struct {
	token Token
}

func (self static) Token() (Token, error) {
	return self.token, nil
}

// Stored is the credentials Login wrote, read from disk and refreshed as they age.
func Stored() TokenSource {
	return &stored{path: CredentialsPath()}
}

// StoredAt is Stored, reading from somewhere other than the usual place.
func StoredAt(path string) TokenSource {
	return &stored{path: path}
}

type stored struct {
	path        string
	mutex       sync.Mutex
	credentials *Credentials
}

// Token reads the credentials on first use and refreshes them when needed.
func (self *stored) Token() (Token, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if self.credentials == nil {
		credentials, err := loadCredentials(self.path)
		if err != nil {
			return Token{}, err
		}

		self.credentials = credentials
	}

	if self.credentials.stale() {
		if err := self.refresh(); err != nil {
			return Token{}, err
		}
	}

	return Token{
		Access:    self.credentials.Access,
		AccountID: self.credentials.AccountID,
	}, nil
}

func (self *stored) refresh() error {
	if self.credentials.Refresh == "" {
		return errors.New("credentials have expired: run the login command again")
	}

	refreshed, err := exchangeRefreshToken(self.credentials.Refresh)
	if err != nil {
		return fmt.Errorf("refresh credentials: %w", err)
	}

	if refreshed.AccountID == "" {
		refreshed.AccountID = self.credentials.AccountID
	}

	self.credentials = refreshed
	_ = saveCredentials(self.path, refreshed)

	return nil
}
