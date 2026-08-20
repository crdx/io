package chat_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"crdx.org/io/agent"
	"crdx.org/io/provider/chat"
	"crdx.org/io/tool"
)

func TestConversationWithStreamingToolCall(t *testing.T) {
	type sentMessage struct {
		Role    string  `json:"role"`
		Content *string `json:"content"`
	}
	type sentRequest struct {
		Model           string        `json:"model"`
		ReasoningEffort string        `json:"reasoning_effort"`
		Messages        []sentMessage `json:"messages"`
	}

	var requests []sentRequest
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("got authorisation %q", got)
		}

		var body sentRequest
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		requests = append(requests, body)

		writer.Header().Set("Content-Type", "text/event-stream")
		if len(requests) == 1 {
			_, _ = fmt.Fprint(writer, "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"check\"}}]}\n\n")
			_, _ = fmt.Fprint(writer, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-1\",\"type\":\"function\",\"function\":{\"name\":\"rea\",\"arguments\":\"{\\\"pa\"}}]}}]}\n\n")
			_, _ = fmt.Fprint(writer, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"name\":\"d\",\"arguments\":\"th\\\":\\\"x\\\"}\"}}]}}]}\n\n")
			_, _ = fmt.Fprint(writer, "data: [DONE]\n\n")
			return
		}

		_, _ = fmt.Fprint(writer, "data: {\"choices\":[{\"delta\":{\"content\":\"done\"}}]}\n\n")
		_, _ = fmt.Fprint(writer, "data: [DONE]\n\n")
	}))
	defer server.Close()

	client := chat.New(server.URL, "deepseek-v4-pro", "high", "secret")
	client.Configure("be useful", []tool.Definition{{Name: "read", Description: "read a file"}})
	client.AddUserMessage("hello")

	var events []agent.Event
	reply, err := client.Send(t.Context(), func(event agent.Event) bool {
		events = append(events, event)
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	wantCall := agent.ToolCall{ID: "call-1", Name: "read", Arguments: `{"path":"x"}`}
	if len(reply.Calls) != 1 || reply.Calls[0] != wantCall {
		t.Fatalf("got calls %#v", reply.Calls)
	}
	if len(events) != 1 || events[0].Kind != agent.Reasoning || events[0].Text != "check" {
		t.Fatalf("got events %#v", events)
	}

	client.AddToolResults([]agent.ToolResult{{ID: "call-1", Output: "contents"}})
	events = nil
	if _, err := client.Send(t.Context(), func(event agent.Event) bool {
		events = append(events, event)
		return true
	}); err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Kind != agent.Text || events[0].Text != "done" {
		t.Fatalf("got events %#v", events)
	}

	if got := requests[0].Model; got != "deepseek-v4-pro" {
		t.Errorf("got model %v", got)
	}
	if got := requests[0].ReasoningEffort; got != "high" {
		t.Errorf("got reasoning effort %v", got)
	}
	messages := requests[1].Messages
	roles := make([]string, len(messages))
	for index, message := range messages {
		roles[index] = message.Role
	}
	if want := []string{"system", "user", "assistant", "tool"}; !slices.Equal(roles, want) {
		t.Errorf("got roles %v", roles)
	}
	if messages[2].Content == nil || *messages[2].Content != "" {
		t.Errorf("got assistant content %v, want an explicit empty string", messages[2].Content)
	}
}
