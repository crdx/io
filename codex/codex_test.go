package codex_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"crdx.org/io/codex"
	"crdx.org/io/harness"
	"crdx.org/io/tool"
)

type WeatherParams struct {
	City string `json:"city"`
}

func events(payloads ...string) string {
	var out strings.Builder

	for _, payload := range payloads {
		fmt.Fprintf(&out, "data: %s\n\n", payload)
	}

	return out.String()
}

func answer(text string) string {
	return fmt.Sprintf(`{"type":"response.output_text.delta","delta":%q}`, text)
}

func call(name string, id string, arguments string) string {
	item := fmt.Sprintf(
		`{"type":"function_call","call_id":%q,"name":%q,"arguments":%q}`, id, name, arguments,
	)

	return fmt.Sprintf(`{"type":"response.output_item.done","item":%s}`, item)
}

const completed = `{"type":"response.completed"}`

func turns(t *testing.T, scripted ...string) (*httptest.Server, *[]string) {
	t.Helper()

	var bodies []string
	var index int

	server := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			body, _ := io.ReadAll(request.Body)
			bodies = append(bodies, string(body))

			if index >= len(scripted) {
				t.Errorf("the endpoint was asked %d times, with %d turns scripted",
					index+1, len(scripted))

				return
			}

			writer.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(writer, scripted[index])
			index++
		},
	))

	t.Cleanup(server.Close)

	return server, &bodies
}

func newAgent(t *testing.T, url string, tools []tool.Tool) *harness.Agent {
	t.Helper()

	backend := codex.New(codex.Static("token", "account"))
	backend.URL = url

	return harness.NewAgent("You are a helpful assistant", backend, tools)
}

func weatherTool(t *testing.T, called *int) tool.Tool {
	t.Helper()

	return tool.Define(
		"weather",
		"report weather in a city",
		tool.Schema{tool.String("city", "the city to look up")},
		func(args WeatherParams) string { return args.City },
		func(args WeatherParams) (string, error) {
			*called++

			if args.City != "London" {
				t.Errorf("expected the city to be London, got %q", args.City)
			}

			return "raining in " + args.City, nil
		},
	)
}

func TestSendRunsToolsUntilTheModelStops(t *testing.T) {
	server, bodies := turns(
		t,
		events(call("weather", "c1", `{"city":"London"}`), completed),
		events(answer("It is raining "), answer("in London."), completed),
	)

	var called int
	agent := newAgent(t, server.URL, []tool.Tool{weatherTool(t, &called)})

	said, err := agent.Send("what is the weather in London?")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if said != "It is raining in London." {
		t.Errorf("expected the answer, got %q", said)
	}

	if called != 1 {
		t.Errorf("expected the tool to run once, ran %d times", called)
	}

	if len(*bodies) != 2 {
		t.Fatalf("expected two requests, got %d", len(*bodies))
	}

	if !strings.Contains((*bodies)[1], "function_call_output") {
		t.Error("expected the second request to carry the tool result")
	}

	if !strings.Contains((*bodies)[0], "You are a helpful assistant") {
		t.Error("expected the request to carry the instructions")
	}

	if !strings.Contains((*bodies)[0], `"name":"weather"`) {
		t.Error("expected the request to declare the tool")
	}
}

func TestSendReportsAnEndpointFailure(t *testing.T) {
	server, _ := turns(
		t,
		events(`{"type":"response.failed","response":{"error":{"message":"model overloaded"}}}`),
	)

	agent := newAgent(t, server.URL, nil)

	if _, err := agent.Send("hello"); err == nil || err.Error() != "model overloaded" {
		t.Errorf("expected the endpoint's own message, got %v", err)
	}
}

func TestSendTellsTheModelWhenThereIsNoSuchTool(t *testing.T) {
	server, bodies := turns(
		t,
		events(call("missing", "c1", `{}`), completed),
		events(answer("Sorry."), completed),
	)

	agent := newAgent(t, server.URL, nil)

	if _, err := agent.Send("hello"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains((*bodies)[1], `there is no tool called \"missing\"`) {
		t.Errorf("expected the model to be told, got %s", (*bodies)[1])
	}
}
