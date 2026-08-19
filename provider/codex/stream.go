package codex

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"

	"crdx.org/io/agent"
	"crdx.org/io/internal/sse"
)

const donePayload = "[DONE]"

// ErrIncomplete is a response the endpoint cut short, usually against a limit.
var ErrIncomplete = errors.New("the response was cut short")

// ErrTruncated is a stream that stopped without ever saying it was finished, which means the wire
// went quiet mid-turn rather than the model reaching an end.
var ErrTruncated = sse.ErrTruncated

type reply struct {
	items []json.RawMessage // the response output items
	usage agent.Usage       // the context used for the request
}

func (self *reply) calls() []agent.ToolCall {
	var calls []agent.ToolCall

	for _, raw := range self.items {
		var item outputItem

		if json.Unmarshal(raw, &item) == nil && item.Type == "function_call" {
			calls = append(calls, agent.ToolCall{
				ID:        item.CallID,
				Name:      item.Name,
				Arguments: item.Arguments,
			})
		}
	}

	return calls
}

type outputItem struct {
	Type      string `json:"type"`      // the kind of item
	CallID    string `json:"call_id"`   // which call was made
	Name      string `json:"name"`      // which tool was called
	Arguments string `json:"arguments"` // what it was called with
}

type event struct {
	Type     string          `json:"type"`     // what happened
	Delta    string          `json:"delta"`    // the text added
	Message  string          `json:"message"`  // an endpoint failure
	Error    *eventError     `json:"error"`    // a direct endpoint failure
	Item     json.RawMessage `json:"item"`     // a completed output item
	Part     *eventPart      `json:"part"`     // a completed content part
	Response *eventResponse  `json:"response"` // the completed response
}

type eventPart struct {
	Text string `json:"text"` // the completed text
}

type eventResponse struct {
	Error *eventError `json:"error"` // why the response failed
	Usage agent.Usage `json:"usage"` // the context used for the request
}

type eventError struct {
	Message string `json:"message"` // what went wrong
}

func (self *event) failure(payload string) error {
	if self.Response != nil && self.Response.Error != nil && self.Response.Error.Message != "" {
		return errors.New(self.Response.Error.Message)
	}

	if self.Error != nil && self.Error.Message != "" {
		return errors.New(self.Error.Message)
	}

	if self.Message != "" {
		return errors.New(self.Message)
	}

	return errors.New(prettyJSON(payload))
}

func prettyJSON(payload string) string {
	var formattedJSON bytes.Buffer
	if err := json.Indent(&formattedJSON, []byte(payload), "", "  "); err != nil {
		return payload
	}

	return formattedJSON.String()
}

func readReply(body io.Reader, yield agent.Yield) (reply, error) {
	var turn reply

	err := sse.Read(body, func(payload string) (bool, error) {
		return turn.step(payload, yield)
	})

	return turn, err
}

func parseEvent(payload string) (event, bool) {
	var message event
	if json.Unmarshal([]byte(payload), &message) == nil {
		return message, true
	}

	var envelope struct {
		Type string `json:"type"`
	}
	if json.Unmarshal([]byte(payload), &envelope) != nil {
		return event{}, false
	}

	return event{Type: envelope.Type}, true
}

func (self *reply) step(payload string, yield agent.Yield) (bool, error) {
	if payload == donePayload {
		return true, nil
	}

	message, readable := parseEvent(payload)
	if !readable {
		return false, nil
	}

	switch message.Type {
	case "response.output_text.delta":
		if !yield(agent.Event{Kind: agent.Text, Text: message.Delta}) {
			return true, nil
		}

	case "response.reasoning_summary_part.done":
		if message.Part != nil && message.Part.Text != "" {
			if !yield(agent.Event{Kind: agent.Reasoning, Text: message.Part.Text}) {
				return true, nil
			}
		}

	case "response.output_item.done":
		if len(message.Item) > 0 {
			self.items = append(self.items, message.Item)
		}

	case "response.completed", "response.done":
		if message.Response != nil {
			self.usage = message.Response.Usage
		}
		return true, nil

	case "response.incomplete":
		return true, ErrIncomplete

	case "response.failed", "error":
		return true, message.failure(payload)
	}

	return false, nil
}
