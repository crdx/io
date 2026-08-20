package chat

import (
	"encoding/json"
	"errors"
	"io"
	"sort"

	"crdx.org/io/agent"
	"crdx.org/io/internal/sse"
)

const donePayload = "[DONE]"

// ErrTruncated is a stream that stopped without a completion marker.
var ErrTruncated = sse.ErrTruncated

type reply struct {
	content   string
	reasoning string
	tools     map[int]*toolCall
}

func (self *reply) message() message {
	return message{
		Role:             "assistant",
		Content:          self.content,
		ReasoningContent: self.reasoning,
		ToolCalls:        self.orderedToolCalls(),
	}
}

func (self *reply) calls() []agent.ToolCall {
	toolCalls := self.orderedToolCalls()
	calls := make([]agent.ToolCall, len(toolCalls))
	for index, call := range toolCalls {
		calls[index] = agent.ToolCall{
			ID:        call.ID,
			Name:      call.Function.Name,
			Arguments: call.Function.Arguments,
		}
	}
	return calls
}

func (self *reply) orderedToolCalls() []toolCall {
	indexes := make([]int, 0, len(self.tools))
	for index := range self.tools {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)

	calls := make([]toolCall, 0, len(indexes))
	for _, index := range indexes {
		calls = append(calls, *self.tools[index])
	}
	return calls
}

type chunk struct {
	Choices []choice    `json:"choices"`
	Error   *chunkError `json:"error"`
}

type choice struct {
	Delta delta `json:"delta"`
}

type delta struct {
	Content          string          `json:"content"`
	ReasoningContent string          `json:"reasoning_content"`
	Reasoning        string          `json:"reasoning"`
	ToolCalls        []toolCallDelta `json:"tool_calls"`
}

type toolCallDelta struct {
	Index    int           `json:"index"`
	ID       string        `json:"id"`
	Type     string        `json:"type"`
	Function functionDelta `json:"function"`
}

type toolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function functionCall `json:"function"`
}

type functionDelta struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type functionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type chunkError struct {
	Message string `json:"message"`
}

func readReply(body io.Reader, yield agent.Yield) (reply, error) {
	answer := reply{tools: make(map[int]*toolCall)}
	err := sse.Read(body, func(payload string) (bool, error) {
		return answer.step(payload, yield)
	})
	return answer, err
}

func (self *reply) step(payload string, yield agent.Yield) (bool, error) {
	if payload == donePayload {
		return true, nil
	}

	var message chunk
	if json.Unmarshal([]byte(payload), &message) != nil {
		return false, nil //nolint:nilerr // unrelated SSE events are ignored
	}
	if message.Error != nil {
		return true, errors.New(message.Error.Message)
	}
	if len(message.Choices) == 0 {
		return false, nil
	}

	delta := message.Choices[0].Delta
	reasoning := delta.ReasoningContent
	if reasoning == "" {
		reasoning = delta.Reasoning
	}
	if reasoning != "" {
		self.reasoning += reasoning
		if !yield(agent.Event{Kind: agent.Reasoning, Text: reasoning}) {
			return true, nil
		}
	}
	if delta.Content != "" {
		self.content += delta.Content
		if !yield(agent.Event{Kind: agent.Text, Text: delta.Content}) {
			return true, nil
		}
	}

	for _, fragment := range delta.ToolCalls {
		call := self.tools[fragment.Index]
		if call == nil {
			call = &toolCall{}
			self.tools[fragment.Index] = call
		}
		if fragment.ID != "" {
			call.ID = fragment.ID
		}
		if fragment.Type != "" {
			call.Type = fragment.Type
		}
		call.Function.Name += fragment.Function.Name
		call.Function.Arguments += fragment.Function.Arguments
	}

	return false, nil
}
