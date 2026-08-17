package codex_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"crdx.org/io/provider/codex"
)

func writeCredentials(t *testing.T, expiresIn time.Duration) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "auth.json")

	credentials := fmt.Sprintf(
		`{"access":"old","refresh":"refresh-me","account_id":"account","expires_at":%d}`,
		time.Now().Add(expiresIn).UnixMilli(),
	)

	if err := os.WriteFile(path, []byte(credentials), 0o600); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return path
}

func tokenEndpoint(t *testing.T, grants *[]string) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			if err := request.ParseForm(); err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			*grants = append(*grants, request.PostForm.Get("grant_type"))

			writer.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(writer, `{"access_token":"new","refresh_token":"next","expires_in":3600}`)
		},
	))

	t.Cleanup(server.Close)

	codex.TokenURL = server.URL
	t.Cleanup(func() { codex.TokenURL = "" })
}

func TestStoredRefreshesATokenNearExpiry(t *testing.T) {
	var grants []string

	tokenEndpoint(t, &grants)
	path := writeCredentials(t, time.Minute)

	token, err := codex.StoredCredentialsAt(path).Token()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if token.Access != "new" {
		t.Errorf("expected the refreshed token, got %q", token.Access)
	}

	if token.AccountID != "account" {
		t.Errorf("expected the account to survive the refresh, got %q", token.AccountID)
	}

	if len(grants) != 1 || grants[0] != "refresh_token" {
		t.Errorf("expected one refresh_token grant, got %v", grants)
	}

	data, err := os.ReadFile(path) //nolint:gosec // the path is the test's own
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var credentials codex.Credentials
	if err := json.Unmarshal(data, &credentials); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if credentials.Access != "new" || credentials.Refresh != "next" {
		t.Errorf("expected the refreshed pair to be written back, got %+v", credentials)
	}
}

func TestStoredKeepsTheRefreshTokenTheResponseOmits(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(writer, `{"access_token":"new","expires_in":3600}`)
		},
	))

	t.Cleanup(server.Close)

	codex.TokenURL = server.URL
	t.Cleanup(func() { codex.TokenURL = "" })

	path := writeCredentials(t, time.Minute)

	if _, err := codex.StoredCredentialsAt(path).Token(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(path) //nolint:gosec // the path is the test's own
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var credentials codex.Credentials
	if err := json.Unmarshal(data, &credentials); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if credentials.Refresh != "refresh-me" {
		t.Errorf("expected the held refresh token to survive, got %q", credentials.Refresh)
	}
}

func TestStoredLeavesAGoodTokenAlone(t *testing.T) {
	var grants []string

	tokenEndpoint(t, &grants)
	path := writeCredentials(t, time.Hour)

	token, err := codex.StoredCredentialsAt(path).Token()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if token.Access != "old" {
		t.Errorf("expected the stored token, got %q", token.Access)
	}

	if len(grants) != 0 {
		t.Errorf("expected no refresh, got %v", grants)
	}
}

func TestStoredReportsMissingCredentials(t *testing.T) {
	source := codex.StoredCredentialsAt(filepath.Join(t.TempDir(), "absent.json"))

	if _, err := source.Token(); err == nil {
		t.Error("expected an error when there are no credentials")
	}
}
