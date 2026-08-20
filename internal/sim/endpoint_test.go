package sim_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"crdx.org/io/agent"
	"crdx.org/io/internal/sim"
	"crdx.org/io/provider/codex"
	"crdx.org/io/tool"
)

type params struct {
	City string `json:"city"`
}

func weather(callCount *int) tool.Tool {
	return tool.Implement(
		tool.Definition{
			Name:        "weather",
			Description: "report weather in a city",
			Schema:      tool.Schema{tool.String("city", "the city to look up")},
		},
		func(args params) (string, string) { return args.City, "" },
	).Plain(func(_ context.Context, args params) (string, error) {
		*callCount++
		return "raining in " + args.City, nil
	})
}

func serve(t *testing.T, scenario *sim.Scenario) (*sim.Endpoint, string) {
	t.Helper()

	endpoint := sim.New(scenario)
	server := httptest.NewServer(endpoint)

	t.Cleanup(server.Close)

	return endpoint, server.URL
}

func newAgent(url string, tools []tool.Tool) *agent.Agent {
	client := codex.New(codex.Static("token", "account"))
	client.URL = url
	client.Model = "fake"

	return agent.New("You are a helpful assistant", client, tools)
}

var conversation = &sim.Scenario{
	Model:  "fake",
	Strict: true,
	Turns: []sim.Turn{
		{
			Think: []string{"They want the weather."},
			Say:   "Let me look. ",
			Calls: []sim.Call{{Name: "weather", Arguments: `{"city":"London"}`}},
		},
		{
			Say: "It is raining in London.",
		},
	},
}

func TestTheScenarioIsPlayedOneTurnPerRequest(t *testing.T) {
	endpoint, url := serve(t, conversation)

	var callCount int

	answer, err := newAgent(url, []tool.Tool{weather(&callCount)}).Send(t.Context(), "what is the weather in London?")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if want := "Let me look. It is raining in London."; answer != want {
		t.Errorf("expected %q, got %q", want, answer)
	}

	if callCount != 1 {
		t.Errorf("expected the tool to run once, ran %d times", callCount)
	}

	askedRequests := endpoint.Requests()
	if len(askedRequests) != 2 {
		t.Fatalf("expected two requests, got %d", len(askedRequests))
	}

	if askedRequests[0].Session != askedRequests[1].Session {
		t.Errorf("expected one session, got %q then %q", askedRequests[0].Session, askedRequests[1].Session)
	}

	if !carries(askedRequests[1].Input, "function_call_output") {
		t.Errorf("expected the second request to answer the call, got %v", askedRequests[1].Input)
	}
}

func TestACallWithNoOutputIsRefused(t *testing.T) {
	_, url := serve(t, conversation)

	body, err := json.Marshal(map[string]any{
		"model":            "fake",
		"stream":           true,
		"store":            false,
		"instructions":     "You are a helpful assistant",
		"prompt_cache_key": "abandoned",
		"input": []any{
			map[string]any{"role": "user", "content": "what is the weather in London?"},
			map[string]any{
				"type":      "function_call",
				"call_id":   "call_0",
				"name":      "weather",
				"arguments": `{"city":"London"}`,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	//nolint:gosec // the url is the test's own server
	response, err := http.Post(url, "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusBadRequest {
		t.Errorf("expected the request to be refused, got %d", response.StatusCode)
	}

	var refusal struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.NewDecoder(response.Body).Decode(&refusal); err != nil {
		t.Fatal(err)
	}

	if want := "No tool output found for function call call_0."; refusal.Error.Message != want {
		t.Errorf("expected %q, got %q", want, refusal.Error.Message)
	}
}

func TestATurnDroppedMidCallDoesNotSpoilTheNext(t *testing.T) {
	_, url := serve(t, conversation)

	var callCount int

	assistant := newAgent(url, []tool.Tool{weather(&callCount)})

	for event, err := range assistant.Stream(t.Context(), "what is the weather in London?") {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if event.Kind == agent.Call {
			break
		}
	}

	if callCount != 0 {
		t.Errorf("expected the tool not to have run, ran %d times", callCount)
	}

	if _, err := assistant.Send(t.Context(), "never mind, what about Paris?"); err != nil {
		t.Fatalf("the endpoint refused the conversation the cancelled turn left behind: %v", err)
	}
}

func TestAScenarioThatRunsOutSaysSo(t *testing.T) {
	_, url := serve(t, &sim.Scenario{Model: "fake", Turns: []sim.Turn{{Say: "Only this."}}})

	assistant := newAgent(url, nil)

	if _, err := assistant.Send(t.Context(), "first"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	answer, err := assistant.Send(t.Context(), "second")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(answer, "nothing more to say") {
		t.Errorf("expected the scenario to say it is over, got %q", answer)
	}
}

func TestAFailedTurnIsReported(t *testing.T) {
	_, url := serve(t, &sim.Scenario{
		Model: "fake",
		Turns: []sim.Turn{{Fail: "the model is overloaded"}},
	})

	if _, err := newAgent(url, nil).Send(t.Context(), "hello"); err == nil {
		t.Error("expected the failure to be reported")
	} else if err.Error() != "the model is overloaded" {
		t.Errorf("expected the endpoint's own words, got %q", err)
	}
}

func TestARawErrorEventIsReportedAsJSON(t *testing.T) {
	_, url := serve(t, &sim.Scenario{
		Model: "fake",
		Turns: []sim.Turn{{
			ErrorEvent: `{"type":"error","error":{"code":"overloaded","retry":true}}`,
		}},
	})

	_, err := newAgent(url, nil).Send(t.Context(), "hello")
	want := `{
  "type": "error",
  "error": {
    "code": "overloaded",
    "retry": true
  }
}`
	if err == nil || err.Error() != want {
		t.Errorf("expected the raw error event as JSON, got %v", err)
	}
}

func carries(input []sim.Entry, kind string) bool {
	for _, entry := range input {
		if entry.Type == kind {
			return true
		}
	}

	return false
}
