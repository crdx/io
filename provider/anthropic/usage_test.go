package anthropic_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/internal/req"
	"crdx.org/io/provider/anthropic"
)

type countingObserver struct {
	requests int
}

func (self *countingObserver) Start(req.Request) req.ExchangeObserver {
	self.requests++
	return nil
}

func TestTheUsageReportFollowsTheEndpointItWasGiven(t *testing.T) {
	var asked string
	var authorisation string

	server := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			asked = request.URL.Path
			authorisation = request.Header.Get("Authorization")

			writer.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(writer, `{"limits":[`+
				`{"group":"session","percent":42.5,"resets_at":"2026-01-02T15:00:00Z"},`+
				`{"group":"weekly","percent":12,"resets_at":"2026-01-05T00:00:00Z"},`+
				`{"group":"weekly","percent":3,"resets_at":"2026-01-05T00:00:00Z",`+
				`"scope":{"model":{"display_name":"Opus"}}}]}`)
		},
	))

	t.Cleanup(server.Close)

	client := newClient(t, server.URL+"/v1/messages")
	if !client.IsAvailable() {
		t.Fatal("expected the recognised endpoint to support usage reporting")
	}

	observer := &countingObserver{}
	client.ObserveHTTP(observer)

	windows, err := client.UsageWindows(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if observer.requests != 1 {
		t.Errorf("expected the usage poll recorded on the wire, observed %d requests", observer.requests)
	}

	if asked != "/api/oauth/usage" {
		t.Errorf("expected the usage report beside the API root, got %q", asked)
	}

	if authorisation != "Bearer sk-ant-oat-fake" {
		t.Errorf("expected the request authorised with the token, got %q", authorisation)
	}

	want := []agent.UsageWindow{
		{
			Duration: 5 * time.Hour,
			Percent:  42.5,
			ResetsAt: time.Date(2026, 1, 2, 15, 0, 0, 0, time.UTC),
		},
		{
			Duration: 7 * 24 * time.Hour,
			Percent:  12,
			ResetsAt: time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC),
		},
		{
			Duration: 7 * 24 * time.Hour,
			Percent:  3,
			ResetsAt: time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC),
			Scope:    "opus",
		},
	}

	if len(windows) != len(want) {
		t.Fatalf("expected %d windows, got %v", len(want), windows)
	}

	for i, window := range windows {
		if window.Duration != want[i].Duration || window.Percent != want[i].Percent ||
			!window.ResetsAt.Equal(want[i].ResetsAt) || window.Scope != want[i].Scope {
			t.Errorf("window %d: expected %+v, got %+v", i, want[i], window)
		}
	}
}

func TestAMalformedUsageLimitIsDroppedRatherThanGuessedAt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(writer, `{"limits":[`+
				`{"group":"session","percent":10,"resets_at":"not a time"},`+
				`{"group":"monthly","percent":10,"resets_at":"2026-01-02T15:00:00Z"},`+
				`{"group":"weekly","percent":7,"resets_at":"2026-01-05T00:00:00Z"}]}`)
		},
	))

	t.Cleanup(server.Close)

	client := newClient(t, server.URL+"/v1/messages")

	windows, err := client.UsageWindows(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(windows) != 1 || windows[0].Percent != 7 {
		t.Errorf("expected only the weekly window, got %v", windows)
	}
}

func TestAUsageReportIsNotAttemptedAgainstAnUnrecognisedEndpoint(t *testing.T) {
	client := newClient(t, "http://127.0.0.1:1/somewhere/else")

	if client.IsAvailable() {
		t.Fatal("expected the unrecognised endpoint not to support usage reporting")
	}

	windows, err := client.UsageWindows(t.Context())
	if err != nil || windows != nil {
		t.Errorf("expected no report to be attempted, got %v and %v", windows, err)
	}
}

var _ agent.UsageReporter = (*anthropic.Client)(nil)
