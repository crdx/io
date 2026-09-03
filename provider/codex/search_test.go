package codex_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"crdx.org/io/provider/codex"
)

func TestSearchUsesTheCodexBackendAndAuthHeaders(t *testing.T) {
	var requestBody []byte
	var requestHeader http.Header
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestBody, _ = io.ReadAll(request.Body)
		requestHeader = request.Header.Clone()
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(writer, events(answer("sun"), answer("ny"), completed))
	}))
	defer server.Close()

	client, err := codex.NewSearch(codex.Static("token", "account"), "gpt-5.4-mini")
	if err != nil {
		t.Fatal(err)
	}
	client.URL = server.URL

	output, err := client.Search(t.Context(), "current weather")
	if err != nil {
		t.Fatal(err)
	}
	if output != "sunny" {
		t.Errorf("got %q", output)
	}

	for name, want := range map[string]string{
		"Authorization":      "Bearer token",
		"Chatgpt-Account-Id": "account",
		"Originator":         codex.Originator,
		"Openai-Beta":        "responses=experimental",
		"User-Agent":         fmt.Sprintf("oh (%s; %s)", runtime.GOOS, runtime.GOARCH),
	} {
		if got := requestHeader.Get(name); got != want {
			t.Errorf("%s is %q, want %q", name, got, want)
		}
	}
	if requestHeader.Get("Session_id") == "" {
		t.Error("search request has no session ID")
	}

	var body struct {
		Model        string `json:"model"`
		Instructions string `json:"instructions"`
		Input        []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"input"`
		Store     bool `json:"store"`
		Stream    bool `json:"stream"`
		Reasoning struct {
			Effort string `json:"effort"`
		} `json:"reasoning"`
		Tools []struct {
			Type              string `json:"type"`
			SearchContextSize string `json:"search_context_size"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(requestBody, &body); err != nil {
		t.Fatal(err)
	}
	if body.Model != "gpt-5.4-mini" || !body.Stream || body.Store {
		t.Errorf("unexpected request: %#v", body)
	}
	if len(body.Input) != 1 || body.Input[0].Role != "user" || body.Input[0].Content != "current weather" {
		t.Errorf("expected the query as a list of one user message, got %#v", body.Input)
	}
	if body.Instructions == "" {
		t.Error("search request has no instructions")
	}
	if body.Reasoning.Effort != codex.SearchEffort {
		t.Errorf("expected the effort to be stated as %q, got %q", codex.SearchEffort, body.Reasoning.Effort)
	}
	if len(body.Tools) != 1 || body.Tools[0].Type != "web_search" || body.Tools[0].SearchContextSize != "high" {
		t.Errorf("unexpected search tools: %#v", body.Tools)
	}
}

func TestSearchReportsStreamFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(writer, events(`{"type":"error","error":{"message":"quota gone"}}`))
	}))
	defer server.Close()

	client, err := codex.NewSearch(codex.Static("token", "account"), "gpt-5.4-mini")
	if err != nil {
		t.Fatal(err)
	}
	client.URL = server.URL

	if _, err := client.Search(t.Context(), "weather"); err == nil || !strings.Contains(err.Error(), "quota gone") {
		t.Errorf("got %v", err)
	}
}

func TestSearchRequiresAModel(t *testing.T) {
	if client, err := codex.NewSearch(codex.Static("token", "account"), ""); err == nil || client != nil {
		t.Errorf("got client %#v and error %v", client, err)
	}
}
