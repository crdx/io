package backend

import (
	"crdx.org/io/provider/codex"

	"crdx.org/io/cmd/oh/model"
)

func connectCodex(choice model.Choice, effort string, endpoint string) (*Connection, error) {
	tokens := codex.StoredCredentials()
	address := codex.Endpoint

	if endpoint != "" {
		tokens = codex.Static(standInToken, standInToken)
		address = endpoint
	}

	client, err := codex.New(tokens, choice.Model, effort)
	if err != nil {
		return nil, err
	}
	client.URL = address

	return &Connection{Client: client, ToolsSize: codex.ToolsSize}, nil
}
