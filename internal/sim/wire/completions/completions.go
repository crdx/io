// Package completions builds the shapes the Chat Completions API sends, so that everything standing
// in for the endpoint agrees on what it says by construction, rather than by copies of it staying
// in step. https://platform.openai.com/docs/api-reference/chat-streaming
package completions

import (
	"fmt"
)

// Done is the payload that marks the end of a response in this format.
const Done = "[DONE]"

func chunk(delta string) string {
	return `{"object":"chat.completion.chunk","choices":[{"index":0,"delta":` + delta + `}]}`
}

// Answer is a piece of the model's reply arriving.
func Answer(text string) string {
	return chunk(fmt.Sprintf(`{"role":"assistant","content":%q}`, text))
}

// Thought is a piece of the model's reasoning arriving.
func Thought(text string) string {
	return chunk(fmt.Sprintf(`{"role":"assistant","reasoning_content":%q}`, text))
}

// Refusal is what the model says in place of an answer, which this API keeps apart from content.
func Refusal(text string) string {
	return chunk(fmt.Sprintf(`{"role":"assistant","refusal":%q}`, text))
}

// Call is a function call the model is making, whole rather than a fragment at a time.
func Call(index int, id string, name string, arguments string) string {
	return chunk(fmt.Sprintf(
		`{"tool_calls":[{"index":%d,"id":%q,"type":"function",`+
			`"function":{"name":%q,"arguments":%q}}]}`,
		index, id, name, arguments,
	))
}

// Usage reports the context consumed by a completed request.
func Usage(promptTokens int) string {
	return fmt.Sprintf(
		`{"object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":%d}}`,
		promptTokens,
	)
}

// Finish ends a turn, saying why the model stopped.
func Finish(reason string) string {
	return fmt.Sprintf(
		`{"object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":%q}]}`,
		reason,
	)
}

// The reasons a turn ends: naturally, against the token ceiling, or having asked for tools.
const (
	Stopped    = "stop"
	OutOfRoom  = "length"
	AskedTools = "tool_calls"
)

// Error is the endpoint giving up part-way through a stream.
func Error(message string) string {
	return fmt.Sprintf(`{"error":{"message":%q}}`, message)
}
