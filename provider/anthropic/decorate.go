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

func cacheable(messages []message) []json.RawMessage {
	if last := len(messages) - 1; last >= 0 {
		content := messages[last].Content
		if end := len(content) - 1; end >= 0 {
			content[end] = withCacheControl(content[end])
		}
	}

	encoded := make([]json.RawMessage, len(messages))
	for i, item := range messages {
		encoded[i] = encodeItem(item)
	}

	return encoded
}

func withCacheControl(block json.RawMessage) json.RawMessage {
	var fields map[string]any
	if json.Unmarshal(block, &fields) != nil {
		return block
	}

	fields["cache_control"] = map[string]string{"type": "ephemeral"}

	marked, err := json.Marshal(fields)
	if err != nil {
		return block
	}

	return marked
}
