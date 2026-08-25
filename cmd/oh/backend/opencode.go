package backend

import (
	"crdx.org/io/provider/chat"

	"crdx.org/io/cmd/oh/model"
)

const opencodeGoEndpoint = "https://opencode.ai/zen/go/v1/chat/completions"

func connectOpencodeGo(choice model.Choice, effort string, endpoint string) (*Connection, error) {
	token := standInToken

	if endpoint == "" {
		endpoint = opencodeGoEndpoint

		var err error
		if token, err = chat.StoredKey(); err != nil {
			return nil, err
		}
	}

	client, err := chat.New(endpoint, token, choice.Model, effort, choice.MaxOutputTokens)
	if err != nil {
		return nil, err
	}

	return &Connection{Client: client, ToolsSize: chat.ToolsSize}, nil
}
