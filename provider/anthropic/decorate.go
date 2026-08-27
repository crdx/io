package anthropic

import "encoding/json"

func merged(history []json.RawMessage) []message {
	joined := make([]message, 0, len(history))

	for _, item := range history {
		var next message
		if json.Unmarshal(item, &next) != nil || len(next.Content) == 0 {
			continue
		}

		if last := len(joined) - 1; last >= 0 && joined[last].Role == next.Role {
			joined[last].Content = append(joined[last].Content, next.Content...)
			continue
		}

		joined = append(joined, next)
	}

	return joined
}

func encodeMessages(messages []message) []json.RawMessage {
	encoded := make([]json.RawMessage, len(messages))
	for i, item := range messages {
		encoded[i] = encodeItem(item)
	}

	return encoded
}
