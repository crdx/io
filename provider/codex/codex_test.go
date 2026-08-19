package codex_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/provider/codex"
	"crdx.org/io/tool"
)

type WeatherParams struct {
	City string `json:"city"` // the city to report
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

func completedWithUsage(inputTokens int) string {
	return fmt.Sprintf(
		`{"type":"response.completed","response":{"usage":{"input_tokens":%d}}}`,
		inputTokens,
	)
}

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

func weatherTool(t *testing.T, callCount *int) tool.Tool {
	t.Helper()

	return tool.Implement(
		tool.Definition{
			Name:        "weather",
			Description: "report weather in a city",
			Schema:      tool.Schema{tool.String("city", "the city to look up")},
		},
		func(args WeatherParams) (string, string) { return args.City, "" },
	).Plain(func(_ context.Context, args WeatherParams) (string, error) {
		*callCount++

		if args.City != "London" {
			t.Errorf("expected the city to be London, got %q", args.City)
		}

		return "raining in " + args.City, nil
	})
}

func TestSendRunsToolsUntilTheModelStops(t *testing.T) {
	server, bodies := turns(
		t,
		events(call("weather", `{"city":"London"}`), completed),
		events(answer("It is raining "), answer("in London."), completed),
	)

	var callCount int
	assistant := newAgent(t, server.URL, []tool.Tool{weatherTool(t, &callCount)})

	answer, err := assistant.Send(t.Context(), "what is the weather in London?")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if answer != "It is raining in London." {
		t.Errorf("expected the answer, got %q", answer)
	}

	if callCount != 1 {
		t.Errorf("expected the tool to run once, ran %d times", callCount)
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

func TestAnImageReturnedByAToolIsSentForTheModelToInspect(t *testing.T) {
	server, bodies := turns(
		t,
		events(call("view", `{}`), completed),
		events(answer("I can see it."), completed),
	)

	type nothing struct{}
	viewTool := tool.Implement(
		tool.Definition{
			Name:        "view",
			Description: "view an image",
			Schema:      tool.Schema{},
		},
		func(nothing) (string, string) { return "picture.png", "" },
	).StatsWithImage(func(context.Context, nothing) (string, tool.Image, tool.Stats, error) {
		return "image/png image (3 bytes)", tool.Image{
			MediaType: "image/png",
			Data:      []byte{1, 2, 3},
		}, tool.Stats{}, nil
	})

	assistant := newAgent(t, server.URL, []tool.Tool{viewTool})
	if _, err := assistant.Send(t.Context(), "inspect the image"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := `"output":[{"type":"input_text","text":"image/png image (3 bytes)"},` +
		`{"type":"input_image","image_url":"data:image/png;base64,AQID","detail":"high"}]`
	if !strings.Contains((*bodies)[1], want) {
		t.Errorf("expected the tool output to carry an image, got %s", (*bodies)[1])
	}
}

func TestToolsAreOfferedInTheResponsesShape(t *testing.T) {
	server, bodies := turns(t, events(answer("Hello."), completed))

	var callCount int
	assistant := newAgent(t, server.URL, []tool.Tool{weatherTool(t, &callCount)})

	if _, err := assistant.Send(t.Context(), "hello"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	toolsJSON := `"tools":[{"type":"function","name":"weather",` +
		`"description":"report weather in a city","strict":false,` +
		`"parameters":{"type":"object","properties":{"city":` +
		`{"type":"string","description":"the city to look up"}},` +
		`"required":["city"],"additionalProperties":false}}]`

	if !strings.Contains((*bodies)[0], toolsJSON) {
		t.Errorf("expected %s, got %s", toolsJSON, (*bodies)[0])
	}
}

func TestAToolWithNoArgumentsIsStillGivenASchema(t *testing.T) {
	server, bodies := turns(t, events(answer("Hello."), completed))

	type nothing struct{}

	waitingTool := tool.Implement(
		tool.Definition{
			Name:        "wait",
			Description: "wait for something to happen",
			Schema:      tool.Schema{},
		},
		func(nothing) (string, string) { return "", "" },
	).Plain(func(context.Context, nothing) (string, error) { return "", nil })

	assistant := newAgent(t, server.URL, []tool.Tool{waitingTool})

	if _, err := assistant.Send(t.Context(), "hello"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	schemaJSON := `"parameters":{"type":"object","properties":{},"additionalProperties":false}`

	if !strings.Contains((*bodies)[0], schemaJSON) {
		t.Errorf("expected %s, got %s", schemaJSON, (*bodies)[0])
	}
}

func TestStreamReportsEachTurnAsItHappens(t *testing.T) {
	server, _ := turns(
		t,
		events(answer("Let me look. "), call("weather", `{"city":"London"}`), completed),
		events(answer("It is raining in London."), completed),
	)

	var callCount int
	assistant := newAgent(t, server.URL, []tool.Tool{weatherTool(t, &callCount)})

	var eventStrings []string

	for event, err := range assistant.Stream(t.Context(), "what is the weather in London?") {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		eventStrings = append(eventStrings, fmt.Sprintf("%s:%s:%s", event.Kind, event.Name, event.Text))
	}

	expectedEvents := []string{
		fmt.Sprintf("%s::what is the weather in London?", agent.Prompt),
		fmt.Sprintf("%s::Let me look. ", agent.Text),
		fmt.Sprintf("%s:weather:", agent.Call),
		fmt.Sprintf("%s:weather:raining in London", agent.Result),
		fmt.Sprintf("%s::It is raining in London.", agent.Text),
	}

	if !slices.Equal(eventStrings, expectedEvents) {
		t.Errorf("expected %v, got %v", expectedEvents, eventStrings)
	}
}

func TestStreamReportsTheFinalRequestsContextUsage(t *testing.T) {
	server, _ := turns(
		t,
		events(call("weather", `{"city":"London"}`), completedWithUsage(12_000)),
		events(answer("It is raining."), completedWithUsage(27_400)),
	)

	var callCount int
	assistant := newAgent(t, server.URL, []tool.Tool{weatherTool(t, &callCount)})

	var usages []agent.Usage
	for event, err := range assistant.Stream(t.Context(), "what is the weather?") {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if event.Kind == agent.ContextUsage && event.Usage != nil {
			usages = append(usages, *event.Usage)
		}
	}

	want := []agent.Usage{{InputTokens: 27_400}}
	if !slices.Equal(usages, want) {
		t.Errorf("got usage %v, want %v", usages, want)
	}
}

func TestStreamStopsWhenTheCallerDoes(t *testing.T) {
	server, bodies := turns(
		t,
		events(answer("It is raining "), call("weather", `{"city":"London"}`), completed),
	)

	var callCount int
	assistant := newAgent(t, server.URL, []tool.Tool{weatherTool(t, &callCount)})

	var eventCount int

	for event := range assistant.Stream(t.Context(), "what is the weather in London?") {
		eventCount++

		if event.Kind == agent.Text {
			break
		}
	}

	if eventCount != 2 {
		t.Errorf("expected the prompt and one answer, got %d events", eventCount)
	}

	if callCount != 0 {
		t.Errorf("expected the tool not to run, ran %d times", callCount)
	}

	if len(*bodies) != 1 {
		t.Errorf("expected one request, got %d", len(*bodies))
	}
}

func TestStreamReportsReasoningApartFromTheAnswer(t *testing.T) {
	server, bodies := turns(
		t,
		events(
			`{"type":"response.reasoning_summary_text.delta","delta":"Chec"}`,
			`{"type":"response.reasoning_summary_text.delta","delta":"king."}`,
			`{"type":"response.reasoning_summary_part.done",`+
				`"part":{"type":"summary_text","text":"Checking the sky."}}`,
			answer("It is raining."),
			completed,
		),
	)

	assistant := newAgent(t, server.URL, nil)

	var reasoningSummaries []string
	var answer strings.Builder

	for event, err := range assistant.Stream(t.Context(), "what is the weather?") {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if event.Kind == agent.Reasoning {
			reasoningSummaries = append(reasoningSummaries, event.Text)
		}

		if event.Kind == agent.Text {
			answer.WriteString(event.Text)
		}
	}

	if want := []string{"Checking the sky."}; !slices.Equal(reasoningSummaries, want) {
		t.Errorf("expected %v, got %v", want, reasoningSummaries)
	}

	if want := "It is raining."; answer.String() != want {
		t.Errorf("expected the answer alone, got %q", answer.String())
	}

	if !strings.Contains((*bodies)[0], `"reasoning":{"effort":"high","summary":"auto"}`) {
		t.Errorf("expected a summary to have been asked for, got %s", (*bodies)[0])
	}
}

func TestSendReportsAnEndpointFailure(t *testing.T) {
	server, _ := turns(
		t,
		events(`{"type":"response.failed","response":{"error":{"message":"model overloaded"}}}`),
	)

	assistant := newAgent(t, server.URL, nil)

	if _, err := assistant.Send(t.Context(), "hello"); err == nil || err.Error() != "model overloaded" {
		t.Errorf("expected the endpoint's own message, got %v", err)
	}
}

func TestSendReportsADirectEndpointFailure(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		message string
	}{
		{
			name:    "server error",
			payload: `{"type":"error","error":{"type":"server_error","code":"server_error","message":"An error occurred while processing your request. You can retry your request, or contact us through our help center at help.openai.com if the error persists. Please include the request ID 0e133235-2da5-47a2-8300-2049027f6968 in your message.","param":null},"sequence_number":2}`,
			message: "An error occurred while processing your request. You can retry your request, or contact us through our help center at help.openai.com if the error persists. Please include the request ID 0e133235-2da5-47a2-8300-2049027f6968 in your message.",
		},
		{
			name:    "service unavailable",
			payload: `{"type":"error","error":{"type":"service_unavailable_error","code":"server_is_overloaded","message":"Our servers are currently overloaded. Please try again later.","param":null},"sequence_number":2}`,
			message: "Our servers are currently overloaded. Please try again later.",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server, _ := turns(t, events(test.payload))
			assistant := newAgent(t, server.URL, nil)

			if _, err := assistant.Send(t.Context(), "hello"); err == nil || err.Error() != test.message {
				t.Errorf("expected the endpoint's own message, got %v", err)
			}
		})
	}
}

func TestSendShowsAnEndpointFailureWithoutAMessageAsJSON(t *testing.T) {
	server, _ := turns(t, events(`{"type":"error","error":{"code":"overloaded","retry":true}}`))
	assistant := newAgent(t, server.URL, nil)

	_, err := assistant.Send(t.Context(), "hello")
	want := `{
  "type": "error",
  "error": {
    "code": "overloaded",
    "retry": true
  }
}`
	if err == nil || err.Error() != want {
		t.Errorf("expected the whole event as JSON, got %v", err)
	}
}

func TestSendShowsAnEndpointFailureWithAnUnreadableMessageAsJSON(t *testing.T) {
	server, _ := turns(t, events(`{"type":"error","message":{"text":"model overloaded"}}`))
	assistant := newAgent(t, server.URL, nil)

	_, err := assistant.Send(t.Context(), "hello")
	want := `{
  "type": "error",
  "message": {
    "text": "model overloaded"
  }
}`
	if err == nil || err.Error() != want {
		t.Errorf("expected the whole event as JSON, got %v", err)
	}
}

func TestSendRefusesATruncatedStream(t *testing.T) {
	server, _ := turns(t, events(answer("It is raining ")))

	assistant := newAgent(t, server.URL, nil)

	answer, err := assistant.Send(t.Context(), "what is the weather in London?")
	if err == nil {
		t.Fatal("expected a truncated stream to be refused")
	}

	if answer != "It is raining " {
		t.Errorf("expected what did arrive to be handed back, got %q", answer)
	}
}

func TestSendReportsAnIncompleteResponse(t *testing.T) {
	server, _ := turns(
		t,
		events(answer("It is raining "), `{"type":"response.incomplete"}`),
	)

	assistant := newAgent(t, server.URL, nil)

	answer, err := assistant.Send(t.Context(), "what is the weather in London?")
	if !errors.Is(err, codex.ErrIncomplete) {
		t.Fatalf("expected an incomplete response to be reported, got %v", err)
	}

	if answer != "It is raining " {
		t.Errorf("expected what did arrive to be handed back, got %q", answer)
	}
}

func TestSendAcceptsTheDoneSentinel(t *testing.T) {
	server, _ := turns(t, events(answer("It is raining in London."), "[DONE]"))

	assistant := newAgent(t, server.URL, nil)

	answer, err := assistant.Send(t.Context(), "what is the weather in London?")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if answer != "It is raining in London." {
		t.Errorf("expected the answer, got %q", answer)
	}
}

func TestSendTellsTheModelWhenThereIsNoSuchTool(t *testing.T) {
	server, bodies := turns(
		t,
		events(call("missing", `{}`), completed),
		events(answer("Sorry."), completed),
	)

	assistant := newAgent(t, server.URL, nil)

	if _, err := assistant.Send(t.Context(), "hello"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains((*bodies)[1], `there is no tool called \"missing\"`) {
		t.Errorf("expected the model to be told, got %s", (*bodies)[1])
	}
}

func TestCancellingTheContextEndsARequestThatIsProducingNothing(t *testing.T) {
	requestDone := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			writer.Header().Set("Content-Type", "text/event-stream")

			if flusher, ok := writer.(http.Flusher); ok {
				flusher.Flush()
			}

			<-request.Context().Done()
			close(requestDone)
		},
	))

	t.Cleanup(server.Close)

	ctx, stop := context.WithCancel(t.Context())

	failure := make(chan error, 1)

	go func() {
		_, err := newAgent(t, server.URL, nil).Send(ctx, "are you there")
		failure <- err
	}()

	time.Sleep(50 * time.Millisecond)
	stop()

	select {
	case err := <-failure:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected the turn to report the cancellation, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected the request to end when the context was cancelled")
	}

	select {
	case <-requestDone:
	case <-time.After(2 * time.Second):
		t.Error("expected the endpoint to see the connection go")
	}
}
