package messages

import (
	"testing"

	"crdx.org/io/agent"
)

func TestAConversationChangeDropsToolInputCorrection(t *testing.T) {
	mutations := map[string]func(*Client){
		"user message": func(client *Client) {
			client.AddUserMessage("A new turn.")
		},
		"tool results": func(client *Client) {
			client.AddToolResults([]agent.ToolCallResult{{}})
		},
		"loaded history": func(client *Client) {
			client.Load(nil)
		},
	}

	for name, mutation := range mutations {
		t.Run(name, func(t *testing.T) {
			client := &Client{toolInputCorrection: "Correct the old turn."}
			mutation(client)
			if client.toolInputCorrection != "" {
				t.Errorf("correction survived: %q", client.toolInputCorrection)
			}
		})
	}
}
