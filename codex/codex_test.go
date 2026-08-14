package codex_test

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"crdx.org/io/agent"
	"crdx.org/io/codex"
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

func call(name string, arguments string) string {
	item := fmt.Sprintf(
		`{"type":"function_call","call_id":%q,"name":%q,"arguments":%q}`, callID, name, arguments,
	)

	return fmt.Sprintf(`{"type":"response.output_item.done","item":%s}`, item)
}

const callID = "c1"

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

func newAgent(t *testing.T, url string, tools []tool.Tool) *agent.Agent {
	t.Helper()

	backend := codex.New(codex.Static("token", "account"))
	backend.URL = url

	return agent.New("You are a helpful assistant", backend, tools)
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
		events(call("weather", `{"city":"London"}`), completed),
		events(answer("It is raining "), answer("in London."), completed),
	)

	var called int
	assistant := newAgent(t, server.URL, []tool.Tool{weatherTool(t, &called)})

	said, err := assistant.Send("what is the weather in London?")
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

// The Responses API takes a tool flat, where the older chat one nests it under a function key, so
// what a tool is and how this endpoint is told about it are not the same thing.
func TestToolsAreOfferedInTheResponsesShape(t *testing.T) {
	server, bodies := turns(t, events(answer("Hello."), completed))

	var called int
	assistant := newAgent(t, server.URL, []tool.Tool{weatherTool(t, &called)})

	if _, err := assistant.Send("hello"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	offered := `"tools":[{"type":"function","name":"weather",` +
		`"description":"report weather in a city","strict":false,` +
		`"parameters":{"type":"object","properties":{"city":` +
		`{"type":"string","description":"the city to look up"}},` +
		`"required":["city"],"additionalProperties":false}}]`

	if !strings.Contains((*bodies)[0], offered) {
		t.Errorf("expected %s, got %s", offered, (*bodies)[0])
	}
}

// A tool that takes no arguments still needs a schema, or the endpoint is offered a function with
// no parameters member at all.
func TestAToolWithNoArgumentsIsStillGivenASchema(t *testing.T) {
	server, bodies := turns(t, events(answer("Hello."), completed))

	type nothing struct{}

	waiting := tool.Define(
		"wait",
		"wait for something to happen",
		tool.Schema{},
		func(nothing) string { return "" },
		func(nothing) (string, error) { return "", nil },
	)

	assistant := newAgent(t, server.URL, []tool.Tool{waiting})

	if _, err := assistant.Send("hello"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	offered := `"parameters":{"type":"object","properties":{},"additionalProperties":false}`

	if !strings.Contains((*bodies)[0], offered) {
		t.Errorf("expected %s, got %s", offered, (*bodies)[0])
	}
}

func TestStreamReportsEachTurnAsItHappens(t *testing.T) {
	server, _ := turns(
		t,
		events(answer("Let me look. "), call("weather", `{"city":"London"}`), completed),
		events(answer("It is raining in London."), completed),
	)

	var called int
	assistant := newAgent(t, server.URL, []tool.Tool{weatherTool(t, &called)})

	var seen []string

	for event, err := range assistant.Stream("what is the weather in London?") {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		seen = append(seen, fmt.Sprintf("%d:%s:%s", event.Kind, event.Name, event.Value))
	}

	expected := []string{
		fmt.Sprintf("%d::Let me look. ", agent.Text),
		fmt.Sprintf("%d:weather:", agent.Call),
		fmt.Sprintf("%d:weather:raining in London", agent.Result),
		fmt.Sprintf("%d::It is raining in London.", agent.Text),
	}

	if !slices.Equal(seen, expected) {
		t.Errorf("expected %v, got %v", expected, seen)
	}
}

// A caller that stops listening ends the turn there and then, rather than reading the rest of a
// stream into a conversation nobody is holding any more.
func TestStreamStopsWhenTheCallerDoes(t *testing.T) {
	server, bodies := turns(
		t,
		events(answer("It is raining "), call("weather", `{"city":"London"}`), completed),
	)

	var called int
	assistant := newAgent(t, server.URL, []tool.Tool{weatherTool(t, &called)})

	var seen int

	for range assistant.Stream("what is the weather in London?") {
		seen++
		break
	}

	if seen != 1 {
		t.Errorf("expected one event, got %d", seen)
	}

	if called != 0 {
		t.Errorf("expected the tool not to run, ran %d times", called)
	}

	if len(*bodies) != 1 {
		t.Errorf("expected one request, got %d", len(*bodies))
	}
}

func TestSendReportsAnEndpointFailure(t *testing.T) {
	server, _ := turns(
		t,
		events(`{"type":"response.failed","response":{"error":{"message":"model overloaded"}}}`),
	)

	assistant := newAgent(t, server.URL, nil)

	if _, err := assistant.Send("hello"); err == nil || err.Error() != "model overloaded" {
		t.Errorf("expected the endpoint's own message, got %v", err)
	}
}

// A stream that stops early is not an answer.
func TestSendRefusesATruncatedStream(t *testing.T) {
	server, _ := turns(t, events(answer("It is raining ")))

	assistant := newAgent(t, server.URL, nil)

	said, err := assistant.Send("what is the weather in London?")
	if err == nil {
		t.Fatal("expected a truncated stream to be refused")
	}

	if said != "It is raining " {
		t.Errorf("expected what did arrive to be handed back, got %q", said)
	}
}

// A response cut short by a limit carries real text, so the caller gets it, but must be able to
// tell it apart from one the model chose to end.
func TestSendReportsAnIncompleteResponse(t *testing.T) {
	server, _ := turns(
		t,
		events(answer("It is raining "), `{"type":"response.incomplete"}`),
	)

	assistant := newAgent(t, server.URL, nil)

	said, err := assistant.Send("what is the weather in London?")
	if !errors.Is(err, codex.ErrIncomplete) {
		t.Fatalf("expected an incomplete response to be reported, got %v", err)
	}

	if said != "It is raining " {
		t.Errorf("expected what did arrive to be handed back, got %q", said)
	}
}

// The endpoint closes a stream with this sentinel, so it ends a turn as surely as any event.
func TestSendAcceptsTheDoneSentinel(t *testing.T) {
	server, _ := turns(t, events(answer("It is raining in London."), "[DONE]"))

	assistant := newAgent(t, server.URL, nil)

	said, err := assistant.Send("what is the weather in London?")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if said != "It is raining in London." {
		t.Errorf("expected the answer, got %q", said)
	}
}

func TestSendTellsTheModelWhenThereIsNoSuchTool(t *testing.T) {
	server, bodies := turns(
		t,
		events(call("missing", `{}`), completed),
		events(answer("Sorry."), completed),
	)

	assistant := newAgent(t, server.URL, nil)

	if _, err := assistant.Send("hello"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains((*bodies)[1], `there is no tool called \"missing\"`) {
		t.Errorf("expected the model to be told, got %s", (*bodies)[1])
	}
}
