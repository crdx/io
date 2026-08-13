package codex

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"time"
)

const (
	authoriseURL = "https://auth.openai.com/oauth/authorize"
	tokenURL     = "https://auth.openai.com/oauth/token" //nolint:gosec // an address, not a credential
	clientID     = "app_EMoamEEZ73f0CkXaXp7hrann"
	callbackHost = "127.0.0.1:1455"
	redirectURL  = "http://localhost:1455/auth/callback"
	scope        = "openid profile email offline_access"
)

const loginPatience = 5 * time.Minute

// Login authorises against a ChatGPT subscription and stores the credentials, printing the URL to
// visit and opening it where it can. It returns once the browser has come back to the callback.
func Login() error {
	verifier := newToken()
	state := newToken()

	listener, err := net.Listen("tcp", callbackHost)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", callbackHost, err)
	}

	address := authoriseAddress(verifier, state)

	fmt.Println("Visit this address to authorise:")
	fmt.Println()
	fmt.Println("  " + address)
	fmt.Println()

	open(address)

	code, err := waitForCallback(listener, state)
	if err != nil {
		return err
	}

	credentials, err := exchange(code, verifier)
	if err != nil {
		return err
	}

	return saveCredentials(CredentialsPath(), credentials)
}

func authoriseAddress(verifier string, state string) string {
	digest := sha256.Sum256([]byte(verifier))

	query := url.Values{
		"response_type":              {"code"},
		"client_id":                  {clientID},
		"redirect_uri":               {redirectURL},
		"scope":                      {scope},
		"state":                      {state},
		"code_challenge":             {base64.RawURLEncoding.EncodeToString(digest[:])},
		"code_challenge_method":      {"S256"},
		"id_token_add_organizations": {"true"},
		"codex_cli_simplified_flow":  {"true"},
		"originator":                 {Originator},
	}

	return authoriseURL + "?" + query.Encode()
}

func waitForCallback(listener net.Listener, state string) (string, error) {
	type outcome struct {
		code string
		err  error
	}

	answered := make(chan outcome, 1)

	server := &http.Server{
		ReadHeaderTimeout: 10 * time.Second,
		Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			query := request.URL.Query()

			if query.Get("state") != state {
				http.Error(writer, "state did not match", http.StatusBadRequest)
				answered <- outcome{err: errors.New("the callback state did not match")}

				return
			}

			if code := query.Get("code"); code != "" {
				_, _ = fmt.Fprintln(writer, "Authorised. You can close this tab.")
				answered <- outcome{code: code}

				return
			}

			http.Error(writer, "no code", http.StatusBadRequest)
			answered <- outcome{err: errors.New("the callback carried no code")}
		}),
	}

	go func() { _ = server.Serve(listener) }()

	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		_ = server.Shutdown(ctx)
	}()

	select {
	case answer := <-answered:
		return answer.code, answer.err
	case <-time.After(loginPatience):
		return "", errors.New("gave up waiting to be authorised")
	}
}

func exchange(code string, verifier string) (*Credentials, error) {
	return postForm(url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"code":          {code},
		"redirect_uri":  {redirectURL},
		"code_verifier": {verifier},
	})
}

func exchangeRefreshToken(refresh string) (*Credentials, error) {
	return postForm(url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {clientID},
		"refresh_token": {refresh},
		"scope":         {scope},
	})
}

func postForm(form url.Values) (*Credentials, error) {
	response, err := http.PostForm(tokenEndpoint(), form)
	if err != nil {
		return nil, fmt.Errorf("exchange the code: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return nil, responseError(response)
	}

	var payload struct {
		Access    string `json:"access_token"`
		Refresh   string `json:"refresh_token"`
		ExpiresIn int64  `json:"expires_in"`
	}

	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("parse the token response: %w", err)
	}

	if payload.Access == "" {
		return nil, errors.New("the token response carried no access token")
	}

	return &Credentials{
		Access:    payload.Access,
		Refresh:   payload.Refresh,
		ExpiresAt: time.Now().Add(time.Duration(payload.ExpiresIn) * time.Second).UnixMilli(),
		AccountID: accountID(payload.Access),
	}, nil
}

// TokenURL is the address credentials are traded at, and is the real one when left empty. A test
// setting it points the refresh at somewhere it can answer.
var TokenURL string

func tokenEndpoint() string {
	if TokenURL != "" {
		return TokenURL
	}

	return tokenURL
}

func accountID(access string) string {
	parts := strings.Split(access, ".")
	if len(parts) < 2 {
		return ""
	}

	body, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}

	var claims struct {
		Auth struct {
			AccountID string `json:"chatgpt_account_id"`
		} `json:"https://api.openai.com/auth"`
	}

	if json.Unmarshal(body, &claims) != nil {
		return ""
	}

	return claims.Auth.AccountID
}

func open(address string) {
	_ = exec.Command("xdg-open", address).Start() //nolint:gosec // the address is one we built
}
