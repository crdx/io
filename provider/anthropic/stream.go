package anthropic

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"crdx.org/io/agent"
	"crdx.org/io/internal/sse"
)

// ErrIncomplete is a response the endpoint cut short, usually against a limit.
var ErrIncomplete = errors.New("the response was cut short")

// ErrTruncated is a stream that stopped without ever saying it was finished, which means the wire
// went quiet mid-turn rather than the model reaching an end.
var ErrTruncated = sse.ErrTruncated

const emptyArguments = "{}"

var cutShort = []string{"max_tokens", "model_context_window_exceeded"}

type block struct {
	kind      string
	index     int
	id        string
	name      string
	data      string
	text      strings.Builder
	signature strings.Builder
	arguments strings.Builder
	isDone    bool
}

func (self *block) argumentsOrEmpty() string {
	if self.arguments.Len() == 0 {
		return emptyArguments
	}

	return self.arguments.String()
}

type reply struct {
	blocks     []*block
	stopReason string
}

func (self *reply) find(index int) *block {
	for _, held := range self.blocks {
		if held.index == index {
			return held
		}
	}

	return nil
}

func (self *reply) message() json.RawMessage {
	return self.assemble(true)
}

func (self *reply) prose() json.RawMessage {
	return self.assemble(false)
}

func (self *reply) assemble(shouldIncludeCalls bool) json.RawMessage {
	blocks := make([]json.RawMessage, 0, len(self.blocks))

	for _, held := range self.blocks {
		switch held.kind {
		case "text":
			if held.text.Len() > 0 {
				blocks = append(blocks, encodeItem(textBlock{Type: "text", Text: held.text.String()}))
			}

		case "redacted_thinking":
			blocks = append(blocks, encodeItem(redactedBlock{Type: "redacted_thinking", Data: held.data}))

		case "thinking":
			switch {
			case held.signature.Len() > 0:
				blocks = append(blocks, encodeItem(thinkingBlock{
					Type:      "thinking",
					Thinking:  held.text.String(),
					Signature: held.signature.String(),
				}))
			case held.text.Len() > 0:
				blocks = append(blocks, encodeItem(textBlock{Type: "text", Text: held.text.String()}))
			}

		case "tool_use":
			if held.isDone && shouldIncludeCalls {
				blocks = append(blocks, encodeItem(toolUse{
					Type:  "tool_use",
					ID:    held.id,
					Name:  held.name,
					Input: json.RawMessage(held.argumentsOrEmpty()),
				}))
			}
		}
	}

	if len(blocks) == 0 {
		return nil
	}

	return encodeItem(message{Role: "assistant", Content: blocks})
}

func (self *reply) calls(knownTools []string) []agent.ToolCall {
	var calls []agent.ToolCall

	for _, held := range self.blocks {
		if held.kind == "tool_use" && held.isDone {
			calls = append(calls, agent.ToolCall{
				ID:        held.id,
				Name:      fromClaudeCodeName(held.name, knownTools),
				Arguments: held.argumentsOrEmpty(),
			})
		}
	}

	return calls
}

type thinkingBlock struct {
	Type      string `json:"type"`
	Thinking  string `json:"thinking"`
	Signature string `json:"signature"`
}

type redactedBlock struct {
	Type string `json:"type"`
	Data string `json:"data"`
}

type toolUse struct {
	Type  string          `json:"type"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type event struct {
	Type         string        `json:"type"`
	Index        int           `json:"index"`
	ContentBlock *startedBlock `json:"content_block"`
	Delta        *eventDelta   `json:"delta"`
	Error        *eventError   `json:"error"`
}

type startedBlock struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Name string `json:"name"`
	Data string `json:"data"`
}

type eventDelta struct {
	Type        string `json:"type"`
	Text        string `json:"text"`
	Thinking    string `json:"thinking"`
	Signature   string `json:"signature"`
	PartialJSON string `json:"partial_json"`
	StopReason  string `json:"stop_reason"`
}

type eventError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

func (self *event) failure(payload string) error {
	if self.Error != nil && self.Error.Message != "" {
		return errors.New(self.Error.Message)
	}

	return errors.New(payload)
}

func readReply(body io.Reader, yield agent.Yield) (reply, error) {
	var turn reply

	err := sse.Read(body, func(payload string) (bool, error) {
		return turn.step(payload, yield)
	})

	return turn, err
}

func (self *reply) step(payload string, yield agent.Yield) (bool, error) {
	var message event
	if json.Unmarshal([]byte(payload), &message) != nil {
		return true, fmt.Errorf("the endpoint sent something unreadable: %s", payload)
	}

	switch message.Type {
	case "content_block_start":
		self.open(message)

	case "content_block_delta":
		return self.add(message, yield), nil

	case "content_block_stop":
		return self.close(message, yield), nil

	case "message_delta":
		if message.Delta != nil && message.Delta.StopReason != "" {
			self.stopReason = message.Delta.StopReason
		}

	case "message_stop":
		if slices.Contains(cutShort, self.stopReason) {
			return true, ErrIncomplete
		}

		return true, nil

	case "error":
		return true, message.failure(payload)
	}

	return false, nil
}

func (self *reply) open(message event) {
	if message.ContentBlock == nil {
		return
	}

	opened := &block{
		kind:  message.ContentBlock.Type,
		index: message.Index,
		id:    message.ContentBlock.ID,
		name:  message.ContentBlock.Name,
		data:  message.ContentBlock.Data,
	}

	if opened.kind == "redacted_thinking" {
		opened.isDone = true
	}

	self.blocks = append(self.blocks, opened)
}

func (self *reply) add(message event, yield agent.Yield) bool {
	held := self.find(message.Index)
	if held == nil || message.Delta == nil {
		return false
	}

	switch message.Delta.Type {
	case "text_delta":
		held.text.WriteString(message.Delta.Text)

		return !yield(agent.Event{Kind: agent.ModelMessage, Text: message.Delta.Text})

	case "thinking_delta":
		held.text.WriteString(message.Delta.Thinking)

	case "signature_delta":
		held.signature.WriteString(message.Delta.Signature)

	case "input_json_delta":
		held.arguments.WriteString(message.Delta.PartialJSON)
	}

	return false
}

func (self *reply) close(message event, yield agent.Yield) bool {
	held := self.find(message.Index)
	if held == nil {
		return false
	}

	held.isDone = true

	if held.kind == "thinking" && held.text.Len() > 0 {
		return !yield(agent.Event{Kind: agent.ModelReasoning, Text: held.text.String()})
	}

	return false
}
