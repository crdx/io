// Package messages builds the shapes the Messages API sends, so that everything standing in for the
// endpoint agrees on what it says by construction, rather than by copies of it staying in step.
// https://platform.claude.com/docs/en/api/messages-streaming
package messages

import (
	"fmt"
)

// Start opens a turn, and is where the account of what it read arrives. Cached tokens are reported
// apart from fresh ones, so a reader adding them up is exercised rather than taken on trust.
func Start(model string, fresh int, cached int) string {
	return fmt.Sprintf(
		`{"type":"message_start","message":{"id":"msg_sim","type":"message","role":"assistant",`+
			`"model":%q,"content":[],"stop_reason":null,`+
			`"usage":{"input_tokens":%d,"cache_read_input_tokens":%d,`+
			`"cache_creation_input_tokens":0,"output_tokens":1}}}`,
		model, fresh, cached,
	)
}

// TextStart opens a block of the model's reply.
func TextStart(index int) string {
	return fmt.Sprintf(
		`{"type":"content_block_start","index":%d,"content_block":{"type":"text","text":""}}`, index,
	)
}

// ThinkingStart opens a block of the model's thinking.
func ThinkingStart(index int) string {
	return fmt.Sprintf(
		`{"type":"content_block_start","index":%d,`+
			`"content_block":{"type":"thinking","thinking":""}}`, index,
	)
}

// ToolStart opens a call the model is making.
func ToolStart(index int, id string, name string) string {
	return fmt.Sprintf(
		`{"type":"content_block_start","index":%d,`+
			`"content_block":{"type":"tool_use","id":%q,"name":%q,"input":{}}}`, index, id, name,
	)
}

// Answer is a piece of the model's reply arriving.
func Answer(index int, text string) string {
	return delta(index, fmt.Sprintf(`{"type":"text_delta","text":%q}`, text))
}

// Thought is a piece of the model's thinking arriving.
func Thought(index int, text string) string {
	return delta(index, fmt.Sprintf(`{"type":"thinking_delta","thinking":%q}`, text))
}

// Signature is the seal on a thought, without which the thought cannot be sent back.
func Signature(index int, seal string) string {
	return delta(index, fmt.Sprintf(`{"type":"signature_delta","signature":%q}`, seal))
}

// Arguments is a piece of what a call is being made with.
func Arguments(index int, fragment string) string {
	return delta(index, fmt.Sprintf(`{"type":"input_json_delta","partial_json":%q}`, fragment))
}

func delta(index int, body string) string {
	return fmt.Sprintf(`{"type":"content_block_delta","index":%d,"delta":%s}`, index, body)
}

// BlockStop closes a block.
func BlockStop(index int) string {
	return fmt.Sprintf(`{"type":"content_block_stop","index":%d}`, index)
}

// Stop says why the model stopped, and carries the final account of what the turn read.
func Stop(reason string, fresh int, cached int) string {
	return fmt.Sprintf(
		`{"type":"message_delta","delta":{"stop_reason":%q,"stop_sequence":null},`+
			`"usage":{"input_tokens":%d,"cache_read_input_tokens":%d,`+
			`"cache_creation_input_tokens":0,"output_tokens":42}}`,
		reason, fresh, cached,
	)
}

// MessageStop ends a turn.
const MessageStop = `{"type":"message_stop"}`

// The reasons a turn ends: naturally, against the token ceiling, or having asked for tools.
const (
	EndTurn   = "end_turn"
	OutOfRoom = "max_tokens"
	ToolUse   = "tool_use"
)

// Error is the endpoint giving up part-way through a stream.
func Error(message string) string {
	return fmt.Sprintf(
		`{"type":"error","error":{"type":"api_error","message":%q}}`, message,
	)
}
