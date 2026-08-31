package responses

import (
	"context"
	"strings"

	"crdx.org/io/agent"
)

const responsesSuffix = "/codex/responses"

const modelsSuffix = "/models"

// SupportsResponses reports whether a model ID names a model for the Responses API.
func SupportsResponses(id string) bool {
	for segment := range strings.SplitSeq(id, "-") {
		switch segment {
		case "audio", "embedding", "image", "moderation", "realtime", "transcribe", "tts":
			return false
		}
	}

	return true
}

// Models lists what the endpoint offers.
func (self *Client) Models(ctx context.Context) ([]agent.Model, error) {
	address, listable := modelsAddress(self.URL)
	if !listable {
		return nil, nil
	}

	token, err := self.tokens.Token()
	if err != nil {
		return nil, err
	}

	var payload struct {
		Models []struct {
			Slug  string `json:"slug"`
			Title string `json:"title"`
		} `json:"models"`

		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}

	if err := self.observedRequests().Get(ctx, address, self.headers(token), &payload); err != nil {
		return nil, err
	}

	models := make([]agent.Model, 0, len(payload.Models)+len(payload.Data))

	for _, listedModel := range payload.Models {
		if listedModel.Slug != "" {
			models = append(models, agent.Model{
				ID:   listedModel.Slug,
				Name: listedModel.Title,
			})
		}
	}

	for _, listedModel := range payload.Data {
		if listedModel.ID != "" {
			models = append(models, agent.Model{ID: listedModel.ID})
		}
	}

	return models, nil
}

func modelsAddress(turnAddress string) (string, bool) {
	prefix, found := strings.CutSuffix(turnAddress, responsesSuffix)
	if !found {
		return "", false
	}

	return prefix + modelsSuffix, true
}
