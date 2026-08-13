package codex

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"crdx.org/io/agent"
)

const donePayload = "[DONE]"

// ErrIncomplete is a response the endpoint cut short, usually against a limit.
var ErrIncomplete = errors.New("the response was cut short")

// ErrTruncated is a stream that stopped without ever saying it was finished, which means the wire
// went quiet mid-turn rather than the model reaching an end.
var ErrTruncated = errors.New("the stream ended before the response did")

type reply struct {
	items []json.RawMessage
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
	Type      string `json:"type"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type event struct {
	Type     string          `json:"type"`
	Delta    string          `json:"delta"`
	Message  string          `json:"message"`
	Item     json.RawMessage `json:"item"`
	Response *eventResponse  `json:"response"`
}

type eventResponse struct {
	Error *eventError `json:"error"`
}

type eventError struct {
	Message string `json:"message"`
	Code    string `json:"code"`
}

func (self *event) failure() error {
	if self.Response != nil && self.Response.Error != nil {
		switch {
		case self.Response.Error.Message != "":
			return errors.New(self.Response.Error.Message)
		case self.Response.Error.Code != "":
			return errors.New(self.Response.Error.Code)
		}
	}

	if self.Message != "" {
		return errors.New(self.Message)
	}

	return fmt.Errorf("the endpoint sent a %s event with no message", self.Type)
}

func readReply(body io.Reader, yield agent.Yield) (reply, error) {
	reader := bufio.NewReader(body)

	var turn reply
	var data strings.Builder

	for {
		line, err := reader.ReadString('\n')
		eof := errors.Is(err, io.EOF)

		if err != nil && !eof {
			return turn, err
		}

		switch text := strings.TrimRight(line, "\r\n"); {
		case strings.HasPrefix(text, "data:"):
			data.WriteString(strings.TrimSpace(strings.TrimPrefix(text, "data:")))

		case text == "":
			payload := data.String()
			data.Reset()

			done, err := turn.step(payload, yield)
			if done || err != nil {
				return turn, err
			}
		}

		if eof {
			switch done, err := turn.step(data.String(), yield); {
			case err != nil:
				return turn, err
			case done:
				return turn, nil
			default:
				return turn, ErrTruncated
			}
		}
	}
}

func parseEvent(payload string) (event, bool) {
	var message event
	return message, json.Unmarshal([]byte(payload), &message) == nil
}

func (self *reply) step(payload string, yield agent.Yield) (bool, error) {
	if payload == "" {
		return false, nil
	}

	if payload == donePayload {
		return true, nil
	}

	message, readable := parseEvent(payload)
	if !readable {
		return false, nil
	}

	switch message.Type {
	case "response.output_text.delta":
		if !yield(message.Delta) {
			return true, nil
		}

	case "response.output_item.done":
		if len(message.Item) > 0 {
			self.items = append(self.items, message.Item)
		}

	case "response.completed", "response.done":
		return true, nil

	case "response.incomplete":
		return true, ErrIncomplete

	case "response.failed", "error":
		return true, message.failure()
	}

	return false, nil
}
