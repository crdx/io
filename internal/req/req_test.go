package req_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"crdx.org/io/internal/req"
)

func refusingServer(t *testing.T, status int, body string) string {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(status)
			_, _ = writer.Write([]byte(body))
		}))

	t.Cleanup(server.Close)

	return server.URL
}

func TestFailureCarriesTheEndpointsOwnMessage(t *testing.T) {
	url := refusingServer(t, http.StatusTooManyRequests, `{"error":{"message":"slow down"}}`)

	_, err := req.New(time.Second).Stream(t.Context(), url, map[string]string{}, nil)
	if err == nil {
		t.Fatal("expected the refusal to be reported")
	}

	if err.Error() != "slow down" {
		t.Errorf("expected the endpoint's own message, got %q", err)
	}
}

func TestFailureFallsBackToTheStatus(t *testing.T) {
	url := refusingServer(t, http.StatusBadGateway, "the gateway is unwell")

	_, err := req.New(time.Second).Stream(t.Context(), url, map[string]string{}, nil)
	if err == nil {
		t.Fatal("expected the refusal to be reported")
	}

	if !strings.Contains(err.Error(), "502") ||
		!strings.Contains(err.Error(), "the gateway is unwell") {
		t.Errorf("expected the status and the body, got %q", err)
	}
}

func TestFormPostsAndDecodes(t *testing.T) {
	var sent string

	server := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			if err := request.ParseForm(); err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			sent = request.PostForm.Get("grant_type")

			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"access_token":"new"}`))
		}))

	t.Cleanup(server.Close)

	var response struct {
		Access string `json:"access_token"` // the token returned
	}

	form := map[string][]string{"grant_type": {"refresh_token"}}

	if err := req.New(time.Second).Form(t.Context(), server.URL, form, &response); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if sent != "refresh_token" {
		t.Errorf("expected the form to arrive, got %q", sent)
	}

	if response.Access != "new" {
		t.Errorf("expected the answer to be read, got %q", response.Access)
	}
}
