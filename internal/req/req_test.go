package req_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
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
		},
	))

	t.Cleanup(server.Close)

	return server.URL
}

func TestFailureCarriesTheEndpointsOwnMessage(t *testing.T) {
	url := refusingServer(t, http.StatusTooManyRequests, `{"error":{"message":"slow down"}}`)

	_, _, err := req.New(time.Second).Stream(t.Context(), url, map[string]string{}, nil)
	if err == nil {
		t.Fatal("expected the refusal to be reported")
	}

	if err.Error() != "slow down" {
		t.Errorf("expected the endpoint's own message, got %q", err)
	}
}

func TestFailureFallsBackToTheStatus(t *testing.T) {
	url := refusingServer(t, http.StatusBadGateway, "the gateway is unwell")

	_, _, err := req.New(time.Second).Stream(t.Context(), url, map[string]string{}, nil)
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
		},
	))

	t.Cleanup(server.Close)

	var response struct {
		Access string `json:"access_token"`
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

func TestARefusalKeepsWhatItWasAsWellAsWhatItSaid(t *testing.T) {
	url := refusingServer(
		t,
		http.StatusTooManyRequests,
		`{"error":{"message":"slow down","code":"rate_limit_exceeded"}}`,
	)

	_, _, err := req.New(time.Second).Stream(t.Context(), url, map[string]string{}, nil)

	var refused *req.StatusError
	if !errors.As(err, &refused) {
		t.Fatalf("expected a refusal the caller can read, got %v", err)
	}

	switch {
	case refused.Status != http.StatusTooManyRequests:
		t.Errorf("expected the status to be kept, got %d", refused.Status)
	case refused.Code != "rate_limit_exceeded":
		t.Errorf("expected the endpoint's own code, got %q", refused.Code)
	case refused.Message != "slow down":
		t.Errorf("expected what it said, got %q", refused.Message)
	case !refused.Retriable():
		t.Error("expected a rate limit to be worth asking again after")
	}
}

func TestARefusalSaysWhetherAskingAgainIsWorthIt(t *testing.T) {
	tests := map[int]bool{
		http.StatusBadRequest:          false,
		http.StatusUnauthorized:        false,
		http.StatusNotFound:            false,
		http.StatusTooManyRequests:     true,
		http.StatusInternalServerError: true,
		http.StatusBadGateway:          true,
		http.StatusServiceUnavailable:  true,
		http.StatusGatewayTimeout:      true,
		529:                            true,
	}

	for status, worthIt := range tests {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			url := refusingServer(t, status, `{"error":{"message":"no"}}`)

			_, _, err := req.New(time.Second).Stream(t.Context(), url, map[string]string{}, nil)

			var refused *req.StatusError
			if !errors.As(err, &refused) {
				t.Fatalf("expected a refusal the caller can read, got %v", err)
			}

			if refused.Retriable() != worthIt {
				t.Errorf("expected retriable %t for %d", worthIt, status)
			}
		})
	}
}

func TestARefusalCarriesHowLongItAskedToBeLeftAloneFor(t *testing.T) {
	tests := map[string]time.Duration{
		"7":                             7 * time.Second,
		" 7 ":                           7 * time.Second,
		"":                              0,
		"whenever":                      0,
		"Mon, 02 Jan 2006 15:04:05 GMT": 0,
	}

	for header, want := range tests {
		t.Run(header, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(
				func(writer http.ResponseWriter, _ *http.Request) {
					if header != "" {
						writer.Header().Set("Retry-After", header)
					}
					writer.WriteHeader(http.StatusTooManyRequests)
					_, _ = writer.Write([]byte(`{"error":{"message":"slow down"}}`))
				}))

			t.Cleanup(server.Close)

			_, _, err := req.New(time.Second).Stream(t.Context(), server.URL, map[string]string{}, nil)

			var refused *req.StatusError
			if !errors.As(err, &refused) {
				t.Fatalf("expected a refusal the caller can read, got %v", err)
			}

			if refused.RetryAfter() != want {
				t.Errorf("expected a wait of %s, got %s", want, refused.RetryAfter())
			}
		})
	}
}

func TestARefusalReadsAWaitGivenAsADate(t *testing.T) {
	asked := time.Now().Add(30 * time.Second).UTC().Format(http.TimeFormat)

	server := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Retry-After", asked)
			writer.WriteHeader(http.StatusServiceUnavailable)
			_, _ = writer.Write([]byte(`{"error":{"message":"back shortly"}}`))
		}))

	t.Cleanup(server.Close)

	_, _, err := req.New(time.Second).Stream(t.Context(), server.URL, map[string]string{}, nil)

	var refused *req.StatusError
	if !errors.As(err, &refused) {
		t.Fatalf("expected a refusal the caller can read, got %v", err)
	}

	if wait := refused.RetryAfter(); wait <= 0 || wait > 30*time.Second {
		t.Errorf("expected a wait of about thirty seconds, got %s", wait)
	}
}
