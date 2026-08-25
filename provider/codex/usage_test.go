package codex_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/provider/codex"
)

var _ agent.UsageReporter = (*codex.Client)(nil)

func newUsageClient(t *testing.T, url string) *codex.Client {
	t.Helper()

	client, err := codex.New(codex.Static("token", "account"), "gpt-5.6-sol", "high")
	if err != nil {
		t.Fatal(err)
	}
	client.URL = url

	return client
}

func rateLimitedTurn(t *testing.T, header map[string]string) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, _ *http.Request) {
			for name, value := range header {
				writer.Header().Set(name, value)
			}

			writer.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(writer, events(message("Hello."), completed))
		},
	))

	t.Cleanup(server.Close)

	return server
}

func TestUsageWindowsCarryWhatTheLastTurnReported(t *testing.T) {
	server := rateLimitedTurn(t, map[string]string{
		"X-Codex-Primary-Used-Percent":        "37.5",
		"X-Codex-Primary-Window-Minutes":      "300",
		"X-Codex-Primary-Resets-In-Seconds":   "3600",
		"X-Codex-Secondary-Used-Percent":      "12",
		"X-Codex-Secondary-Window-Minutes":    "10080",
		"X-Codex-Secondary-Resets-In-Seconds": "86400",
	})

	client := newUsageClient(t, server.URL)
	client.AddUserMessage("hello")

	before := time.Now()
	if _, err := client.Send(t.Context(), func(agent.Output) bool { return true }); err != nil {
		t.Fatal(err)
	}

	if !client.IsAvailable() {
		t.Fatal("expected usage reporting after a turn supplied rate-limit headers")
	}

	windows, err := client.UsageWindows(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if len(windows) != 2 {
		t.Fatalf("expected two windows, got %v", windows)
	}

	if windows[0].Duration != 5*time.Hour || windows[0].Percent != 37.5 {
		t.Errorf("unexpected primary window: %+v", windows[0])
	}

	if windows[1].Duration != 7*24*time.Hour || windows[1].Percent != 12 {
		t.Errorf("unexpected secondary window: %+v", windows[1])
	}

	resetLow := before.Add(time.Hour)
	resetHigh := time.Now().Add(time.Hour)
	if windows[0].ResetsAt.Before(resetLow) || windows[0].ResetsAt.After(resetHigh) {
		t.Errorf("expected the primary reset about an hour out, got %s", windows[0].ResetsAt)
	}
}

func TestAResponseWithoutUsageHeadersKeepsReportingUnavailable(t *testing.T) {
	server := rateLimitedTurn(t, nil)
	client := newUsageClient(t, server.URL)
	client.AddUserMessage("hello")

	if _, err := client.Send(t.Context(), func(agent.Output) bool { return true }); err != nil {
		t.Fatal(err)
	}

	if client.IsAvailable() {
		t.Fatal("expected usage reporting to remain unavailable without rate-limit headers")
	}
}

func TestAResponseWithoutUsageHeadersLeavesTheLastSnapshotStanding(t *testing.T) {
	first := rateLimitedTurn(t, map[string]string{
		"X-Codex-Primary-Used-Percent":   "50",
		"X-Codex-Primary-Window-Minutes": "300",
	})

	client := newUsageClient(t, first.URL)
	client.AddUserMessage("hello")

	if _, err := client.Send(t.Context(), func(agent.Output) bool { return true }); err != nil {
		t.Fatal(err)
	}

	bare := rateLimitedTurn(t, nil)
	client.URL = bare.URL
	client.AddUserMessage("again")

	if _, err := client.Send(t.Context(), func(agent.Output) bool { return true }); err != nil {
		t.Fatal(err)
	}

	windows, err := client.UsageWindows(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if len(windows) != 1 || windows[0].Percent != 50 {
		t.Errorf("expected the first snapshot kept, got %v", windows)
	}
}

func TestThereAreNoUsageWindowsBeforeTheFirstTurn(t *testing.T) {
	client := newUsageClient(t, "http://127.0.0.1:1/nowhere")

	if client.IsAvailable() {
		t.Fatal("expected usage reporting to be unavailable before a turn supplies rate-limit headers")
	}

	windows, err := client.UsageWindows(t.Context())
	if err != nil || windows != nil {
		t.Errorf("expected no windows and no error, got %v and %v", windows, err)
	}
}
