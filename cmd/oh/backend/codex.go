package backend

import (
	"crdx.org/io/provider/codex"

	"crdx.org/io/cmd/oh/model"
)

const webSearchModel = "gpt-5.6-terra"

func connectCodex(choice model.Choice, effort string, endpoint string) (*Connection, error) {
	tokens, address := codexCredentials(endpoint)

	client, err := codex.New(tokens, choice.ID, effort)
	if err != nil {
		return nil, err
	}
	client.URL = address

	search, err := newSearchClient(tokens, address)
	if err != nil {
		return nil, err
	}

	return &Connection{Client: client, ToolsSize: codex.ToolsSize, Search: search}, nil
}

func codexCredentials(endpoint string) (codex.TokenSource, string) {
	if endpoint != "" {
		return codex.Static(standInToken, standInToken), endpoint
	}

	return codex.StoredCredentials(), codex.Endpoint
}

func newSearchClient(tokens codex.TokenSource, address string) (*codex.SearchClient, error) {
	search, err := codex.NewSearch(tokens, webSearchModel)
	if err != nil {
		return nil, err
	}
	search.URL = address

	return search, nil
}
