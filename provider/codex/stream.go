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
	items      []json.RawMessage
	usage      agent.Usage
	summarised bool // whether a reasoning summary has been reported this turn
}

func (self *reply) calls() []agent.ToolCall {
	var calls []agent.ToolCall

	for _, raw := range self.items {
		if item := decodeItem(raw); item.Type == "function_call" {
			calls = append(calls, agent.ToolCall{
				ID:        item.CallID,
				Name:      item.Name,
				Arguments: item.Arguments,
			})
		}
	}

	return calls
}

func (self *reply) prose() []json.RawMessage {
	kept := make([]json.RawMessage, 0, len(self.items))

	for _, raw := range self.items {
		if decodeItem(raw).Type != "function_call" {
			kept = append(kept, raw)
		}
	}

	for len(kept) > 0 && decodeItem(kept[len(kept)-1]).Type == "reasoning" {
		kept = kept[:len(kept)-1]
	}

	return kept
}

func decodeItem(raw json.RawMessage) outputItem {
	var item outputItem
	_ = json.Unmarshal(raw, &item)

	return item
}

type outputItem struct {
	Type      string `json:"type"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type event struct {
	Type     string          `json:"type"`
	Delta    string          `json:"delta"`
	Message  string          `json:"message"`
	Error    *eventError     `json:"error"`
	Item     json.RawMessage `json:"item"`
	Part     *eventPart      `json:"part"`
	Response *eventResponse  `json:"response"`
}

type eventPart struct {
	Text string `json:"text"`
}

type eventResponse struct {
	Error *eventError `json:"error"`
	Usage agent.Usage `json:"usage"`
}

type eventError struct {
	Message string `json:"message"`
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
			self.summarised = true

			if !yield(agent.Event{Kind: agent.Reasoning, Text: message.Part.Text}) {
				return true, nil
			}
		}

	case "response.reasoning_text.delta":
		if message.Delta != "" && !self.summarised {
			if !yield(agent.Event{Kind: agent.Reasoning, Text: message.Delta}) {
				return true, nil
			}
		}

	case "response.refusal.delta":
		if message.Delta != "" {
			if !yield(agent.Event{Kind: agent.Text, Text: message.Delta}) {
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
