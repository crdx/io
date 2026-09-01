package messages

import (
	"fmt"
)

func Start(model string, fresh int, cachedTokens int) string {
	return fmt.Sprintf(
		`{"type":"message_start","message":{"id":"msg_sim","type":"message","role":"assistant",`+
			`"model":%q,"content":[],"stop_reason":null,`+
			`"usage":{"input_tokens":%d,"cache_read_input_tokens":%d,`+
			`"cache_creation_input_tokens":0,"output_tokens":1}}}`,
		model, fresh, cachedTokens,
	)
}

func TextStart(index int) string {
	return fmt.Sprintf(
		`{"type":"content_block_start","index":%d,"content_block":{"type":"text","text":""}}`, index,
	)
}

func ThinkingStart(index int) string {
	return fmt.Sprintf(
		`{"type":"content_block_start","index":%d,`+
			`"content_block":{"type":"thinking","thinking":""}}`, index,
	)
}

func ToolStart(index int, id string, name string) string {
	return fmt.Sprintf(
		`{"type":"content_block_start","index":%d,`+
			`"content_block":{"type":"tool_use","id":%q,"name":%q,"input":{}}}`, index, id, name,
	)
}

func Answer(index int, text string) string {
	return delta(index, fmt.Sprintf(`{"type":"text_delta","text":%q}`, text))
}

func Thought(index int, text string) string {
	return delta(index, fmt.Sprintf(`{"type":"thinking_delta","thinking":%q}`, text))
}

func Signature(index int, seal string) string {
	return delta(index, fmt.Sprintf(`{"type":"signature_delta","signature":%q}`, seal))
}

func Arguments(index int, fragment string) string {
	return delta(index, fmt.Sprintf(`{"type":"input_json_delta","partial_json":%q}`, fragment))
}

func delta(index int, body string) string {
	return fmt.Sprintf(`{"type":"content_block_delta","index":%d,"delta":%s}`, index, body)
}

func BlockStop(index int) string {
	return fmt.Sprintf(`{"type":"content_block_stop","index":%d}`, index)
}

func Stop(reason string, fresh int, cachedTokens int) string {
	return fmt.Sprintf(
		`{"type":"message_delta","delta":{"stop_reason":%q,"stop_sequence":null},`+
			`"usage":{"input_tokens":%d,"cache_read_input_tokens":%d,`+
			`"cache_creation_input_tokens":0,"output_tokens":42}}`,
		reason, fresh, cachedTokens,
	)
}

const MessageStop = `{"type":"message_stop"}`

const (
	EndTurn   = "end_turn"
	OutOfRoom = "max_tokens"
	ToolUse   = "tool_use"
)

func Error(message string) string {
	return fmt.Sprintf(
		`{"type":"error","error":{"type":"api_error","message":%q}}`, message,
	)
}
