package anthropic

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/internal/sse"
)

// ErrIncomplete is a response the endpoint cut short, usually against a limit.
var ErrIncomplete = errors.New("the response was cut short")

// ErrTruncated is a stream that stopped without ever saying it was finished, which means the wire
// went quiet mid-turn rather than the model reaching an end.
var ErrTruncated = sse.ErrTruncated

const (
	emptyArguments = "{}"
	refusalReason  = "refusal"
)

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

func (self *block) hasObjectArguments() bool {
	var object map[string]json.RawMessage

	return json.Unmarshal([]byte(self.argumentsOrEmpty()), &object) == nil && object != nil
}

type reply struct {
	blocks          []*block
	stopReason      string
	stopExplanation string
	usage           agent.Usage
}

type usage struct {
	InputTokens   int `json:"input_tokens"`
	CacheRead     int `json:"cache_read_input_tokens"`
	CacheCreation int `json:"cache_creation_input_tokens"`
}

func (self usage) contextTokens() int {
	return self.InputTokens + self.CacheRead + self.CacheCreation
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
			if held.signature.Len() > 0 {
				blocks = append(blocks, encodeItem(thinkingBlock{
					Type:      "thinking",
					Thinking:  held.text.String(),
					Signature: held.signature.String(),
				}))
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

	return encodeItem(message{Role: assistantRole, Content: blocks})
}

func (self *reply) validateToolInputs() error {
	for _, held := range self.blocks {
		if held.kind == "tool_use" && held.isDone && !held.hasObjectArguments() {
			return invalidToolInputError{toolName: held.name}
		}
	}

	return nil
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
	Type         string          `json:"type"`
	Index        int             `json:"index"`
	ContentBlock *startedBlock   `json:"content_block"`
	Delta        *eventDelta     `json:"delta"`
	Error        *eventError     `json:"error"`
	Message      *startedMessage `json:"message"`
	Usage        *usage          `json:"usage"`
}

type startedMessage struct {
	Usage *usage `json:"usage"`
}

type startedBlock struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Name string `json:"name"`
	Data string `json:"data"`
}

type eventDelta struct {
	Type        string       `json:"type"`
	Text        string       `json:"text"`
	Thinking    string       `json:"thinking"`
	Signature   string       `json:"signature"`
	PartialJSON string       `json:"partial_json"`
	StopReason  string       `json:"stop_reason"`
	StopDetails *stopDetails `json:"stop_details"`
}

type stopDetails struct {
	Explanation string `json:"explanation"`
}

type eventError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

var retriableFailures = map[string]bool{
	"overloaded_error": true,
	"api_error":        true,
	"rate_limit_error": true,
	"timeout_error":    true,
}

type streamError struct {
	kind    string
	message string
}

func (self *streamError) Error() string {
	return self.message
}

func (self *streamError) Retriable() bool {
	return retriableFailures[self.kind]
}

func (*streamError) RetryAfter() time.Duration {
	return 0
}

func (self *event) failure(payload string) error {
	if self.Error != nil && self.Error.Message != "" {
		return &streamError{kind: self.Error.Type, message: self.Error.Message}
	}

	return errors.New(payload)
}

func readReply(body io.Reader, yield agent.Yield) (reply, error) {
	var reply reply

	err := sse.Read(body, func(payload string) (bool, error) {
		return reply.step(payload, yield)
	})

	return reply, err
}

func (self *reply) step(payload string, yield agent.Yield) (bool, error) {
	var event event
	if json.Unmarshal([]byte(payload), &event) != nil {
		return true, fmt.Errorf("the endpoint sent something unreadable: %s", payload)
	}

	switch event.Type {
	case "message_start":
		if event.Message != nil {
			self.recordUsage(event.Message.Usage)
		}

	case "content_block_start":
		self.open(event)

	case "content_block_delta":
		return self.add(event, yield), nil

	case "content_block_stop":
		return self.close(event, yield), nil

	case "message_delta":
		if event.Delta != nil {
			if event.Delta.StopReason != "" {
				self.stopReason = event.Delta.StopReason
			}
			if event.Delta.StopDetails != nil {
				self.stopExplanation = event.Delta.StopDetails.Explanation
			}
		}
		self.recordUsage(event.Usage)

	case "message_stop":
		switch {
		case slices.Contains(cutShort, self.stopReason):
			return true, ErrIncomplete
		case self.stopReason == refusalReason && self.stopExplanation != "":
			return true, errors.New(self.stopExplanation)
		case self.stopReason == refusalReason:
			return true, errors.New("the model refused the request")
		default:
			return true, nil
		}

	case "error":
		return true, event.failure(payload)
	}

	return false, nil
}

func (self *reply) recordUsage(usage *usage) {
	if usage == nil {
		return
	}

	if inputTokens := usage.contextTokens(); inputTokens > 0 {
		self.usage = agent.Usage{InputTokens: inputTokens}
	}
}

func (self *reply) open(event event) {
	if event.ContentBlock == nil {
		return
	}

	opened := &block{
		kind:  event.ContentBlock.Type,
		index: event.Index,
		id:    event.ContentBlock.ID,
		name:  event.ContentBlock.Name,
		data:  event.ContentBlock.Data,
	}

	if opened.kind == "redacted_thinking" {
		opened.isDone = true
	}

	self.blocks = append(self.blocks, opened)
}

func (self *reply) add(event event, yield agent.Yield) bool {
	held := self.find(event.Index)
	if held == nil || event.Delta == nil {
		return false
	}

	switch event.Delta.Type {
	case "text_delta":
		held.text.WriteString(event.Delta.Text)

		return !yield(agent.Output{Kind: agent.ModelMessageEvent, Text: event.Delta.Text})

	case "thinking_delta":
		held.text.WriteString(event.Delta.Thinking)

		return !yield(agent.Output{Kind: agent.ModelReasoningEvent, Text: event.Delta.Thinking})

	case "signature_delta":
		held.signature.WriteString(event.Delta.Signature)

	case "input_json_delta":
		held.arguments.WriteString(event.Delta.PartialJSON)
	}

	return false
}

func (self *reply) close(event event, yield agent.Yield) bool {
	held := self.find(event.Index)
	if held == nil {
		return false
	}

	held.isDone = true

	var reportedUsage *agent.Usage
	if self.usage.InputTokens > 0 {
		usage := self.usage
		reportedUsage = &usage
	}

	switch {
	case held.kind == "text" && held.text.Len() > 0:
		return !yield(agent.Output{Kind: agent.ModelMessageEvent, Done: true, Usage: reportedUsage})
	case held.kind == "thinking" && held.text.Len() > 0 && held.signature.Len() > 0:
		return !yield(agent.Output{Kind: agent.ModelReasoningEvent, Done: true, Usage: reportedUsage})
	default:
		return false
	}
}
