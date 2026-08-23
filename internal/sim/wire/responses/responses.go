// Package responses builds the shapes the Responses API sends, so that everything standing in for
// the endpoint agrees on what it says by construction, rather than by copies of it staying in
// step. https://platform.openai.com/docs/api-reference/responses-streaming
package responses

import (
	"fmt"
)

// Done is the payload that marks the end of a response in this format.
const Done = "[DONE]"

// CompletedResponse ends a turn that went to plan.
func CompletedResponse(inputTokens int) string {
	return fmt.Sprintf(
		`{"type":"response.completed","response":{"usage":{"input_tokens":%d}}}`,
		inputTokens,
	)
}

// IncompleteResponse ends a turn the endpoint cut short.
const IncompleteResponse = `{"type":"response.incomplete"}`

// FailedResponse ends a turn the endpoint gave up on.
func FailedResponse(message string) string {
	return fmt.Sprintf(`{"type":"response.failed","response":{"error":{"message":%q}}}`, message)
}

// Error is the endpoint refusing the request outright.
func Error(message string) string {
	return fmt.Sprintf(`{"type":"error","message":%q}`, message)
}

// Answer is a piece of the model's reply arriving.
func Answer(text string) string {
	return fmt.Sprintf(`{"type":"response.output_text.delta","delta":%q}`, text)
}

// Thought is a piece of the model's reasoning summary arriving.
func Thought(text string) string {
	return fmt.Sprintf(`{"type":"response.reasoning_summary_text.delta","delta":%q}`, text)
}

// ThinkingPart is one completed part of the reasoning summary.
func ThinkingPart(text string) string {
	return fmt.Sprintf(
		`{"type":"response.reasoning_summary_part.done","part":{"type":"summary_text","text":%q}}`,
		text,
	)
}

// Item is a completed output item, which is how everything but text reaches the client.
func Item(item string) string {
	return `{"type":"response.output_item.done","item":` + item + `}`
}

// Call is a function call the model is making.
func Call(id string, name string, arguments string) string {
	return Item(fmt.Sprintf(
		`{"type":"function_call","call_id":%q,"name":%q,"arguments":%q}`, id, name, arguments,
	))
}

// Message is what the model said, as the item that carries it in the conversation.
func Message(text string) string {
	return Item(fmt.Sprintf(
		`{"type":"message","role":"assistant","content":[{"type":"output_text","text":%q}]}`, text,
	))
}

// ReasoningItem carries the model's sealed thinking.
func ReasoningItem(id string, summary string) string {
	return Item(fmt.Sprintf(
		`{"type":"reasoning","id":%q,"summary":[{"type":"summary_text","text":%q}],`+
			`"encrypted_content":%q}`,
		id, summary, "sealed:"+summary,
	))
}
