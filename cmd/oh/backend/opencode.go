package backend

import (
	"crdx.org/io/provider/chat"

	"crdx.org/io/cmd/oh/model"
)

func connectOpencodeGo(choice model.Choice, effort string, endpoint string) (*Connection, error) {
	token := standInToken

	if endpoint == "" {
		endpoint = chat.GoEndpoint

		var err error
		if token, err = chat.StoredKey(); err != nil {
			return nil, err
		}
	}

	client, err := chat.New(endpoint, token, choice.Model, effort, choice.MaxOutputTokens)
	if err != nil {
		return nil, err
	}

	if endpoint == chat.GoEndpoint {
		client.UsageURL = chat.GoUsageEndpoint
	}

	return &Connection{Client: client, ToolsSize: chat.ToolsSize}, nil
}
