package messages

import "encoding/json"

const (
	userRole      = "user"
	assistantRole = "assistant"

	continueInstruction = "Your previous response was interrupted. Continue from where you left off."
)

func merged(history []json.RawMessage) []message {
	joinedMessages := make([]message, 0, len(history))

	for _, item := range history {
		var next message
		if json.Unmarshal(item, &next) != nil || len(next.Content) == 0 {
			continue
		}

		if last := len(joinedMessages) - 1; last >= 0 && joinedMessages[last].Role == next.Role {
			joinedMessages[last].Content = append(joinedMessages[last].Content, next.Content...)
			continue
		}

		joinedMessages = append(joinedMessages, next)
	}

	return joinedMessages
}

func continued(messages []message) []message {
	if last := len(messages) - 1; last < 0 || messages[last].Role != assistantRole {
		return messages
	}

	return append(messages, message{
		Role:    userRole,
		Content: []json.RawMessage{encodeItem(textBlock{Type: "text", Text: continueInstruction})},
	})
}

func encodeMessages(messages []message) []json.RawMessage {
	encodedMessages := make([]json.RawMessage, len(messages))
	for i, item := range messages {
		encodedMessages[i] = encodeItem(item)
	}

	return encodedMessages
}
