package wire_test

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"crdx.org/io/cmd/oh/store/wire"
	"crdx.org/io/internal/req"
)

func TestRecorderContinuesSequenceNumbersAfterLongLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wire.http")
	stored := strings.Join([]string{
		"# HTTP transcript\n",
		strings.Repeat("x", 128*1024),
		"\n# exchange 7 start\n",
	}, "")
	if err := os.WriteFile(path, []byte(stored), 0o600); err != nil {
		t.Fatal(err)
	}

	recorder, err := wire.Open(path, wire.Meta{}, func(err error) {
		t.Errorf("unexpected recorder failure: %v", err)
	})
	if err != nil {
		t.Fatal(err)
	}
	recorder.Start(req.Request{Started: time.Unix(2, 0), Method: http.MethodPost})
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}

	transcript, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(transcript), "# exchange 8 start") {
		t.Errorf("expected sequence numbering to continue after the long line")
	}
}

func TestRecorderCensorsHeadersJSONFormsSSEAndBearerText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wire.http")
	recorder, err := wire.Open(path, wire.Meta{Name: "brave-otter", Started: time.Unix(1, 0)}, func(err error) {
		t.Errorf("unexpected recorder failure: %v", err)
	})
	if err != nil {
		t.Fatal(err)
	}

	exchange := recorder.Start(req.Request{
		Started:  time.Unix(2, 0),
		Method:   http.MethodPost,
		URL:      "http://example.test/",
		Protocol: "HTTP/1.1",
		Header: http.Header{
			"Authorization": {"Bearer request-secret"},
			"Content-Type":  {"application/json"},
		},
		Body: []byte(`{"nested":{"access_token":"json-secret","innocent":"kept"}}`),
	})
	exchange.Response(req.Response{
		Received: time.Unix(3, 0),
		Protocol: "HTTP/1.1",
		Status:   "200 OK",
		Code:     200,
		Header:   http.Header{"Content-Type": {"text/event-stream"}},
	})
	exchange.Body([]byte("data: {\"refresh_token\":\"sse-secret\",\"ok\":true}\n\ndata: Bearer response-secret\n"))
	exchange.Finish(time.Unix(4, 0), nil, false)
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}

	stored, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	transcript := string(stored)
	for _, secret := range []string{"request-secret", "json-secret", "sse-secret", "response-secret"} {
		if strings.Contains(transcript, secret) {
			t.Errorf("secret %q survived censorship:\n%s", secret, transcript)
		}
	}
	if strings.Count(transcript, "[REDACTED]") != 4 {
		t.Errorf("expected four replacements, got:\n%s", transcript)
	}
	if !strings.Contains(transcript, `"innocent":"kept"`) || !strings.Contains(transcript, `"ok":true`) {
		t.Errorf("expected benign values to survive, got:\n%s", transcript)
	}
	if !strings.Contains(transcript, "# exchange 1 end") || !strings.Contains(transcript, "completed") {
		t.Errorf("expected a completed exchange marker, got:\n%s", transcript)
	}
}
