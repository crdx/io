package sim

import (
	"encoding/json"
	"fmt"
	"net/http"

	"crdx.org/io/internal/sim/wire/responses"
)

type responsesDialect struct{}

func (self responsesDialect) Name() string {
	return Responses
}

// Path carries codex in it because that is where the ChatGPT backend serves this API, and a client
// finds the model listing by trading the tail of its own turn address for one. Matching is done by
// suffix, so an endpoint serving the same API at a plainer address is recognised here as well.
func (self responsesDialect) Path() string {
	return "/codex/responses"
}

type responsesBody struct {
	Model          string            `json:"model"`
	Stream         bool              `json:"stream"`
	Store          bool              `json:"store"`
	Instructions   string            `json:"instructions"`
	PromptCacheKey string            `json:"prompt_cache_key"`
	Input          []json.RawMessage `json:"input"`
	Tools          []struct {
		Name string `json:"name"`
	} `json:"tools"`
}

func (self responsesDialect) Read(request *http.Request, raw []byte) (Request, bool) {
	var sent responsesBody
	if json.Unmarshal(raw, &sent) != nil {
		return Request{}, false
	}

	key := sent.PromptCacheKey
	if key == "" {
		key = request.Header.Get("Session_id")
	}

	asked := Request{
		API:          self.Name(),
		Session:      key,
		Model:        sent.Model,
		Instructions: sent.Instructions,
		Streaming:    sent.Stream,
		Stored:       sent.Store,
		Input:        responsesEntries(sent.Input),
	}

	for _, offered := range sent.Tools {
		asked.Tools = append(asked.Tools, offered.Name)
	}

	return asked, true
}

func responsesEntries(items []json.RawMessage) []Entry {
	read := make([]Entry, 0, len(items))

	for _, item := range items {
		var sent struct {
			Type    string `json:"type"`
			Role    string `json:"role"`
			Content any    `json:"content"`
			CallID  string `json:"call_id"`
			Name    string `json:"name"`
			Output  string `json:"output"`
		}

		_ = json.Unmarshal(item, &sent)

		entry := Entry{
			Type:    sent.Type,
			Role:    sent.Role,
			Content: flatten(sent.Content),
			CallID:  sent.CallID,
			Name:    sent.Name,
			Output:  sent.Output,
			Raw:     item,
		}

		if entry.Type == "" && entry.Role != "" {
			entry.Type = Message
		}

		read = append(read, entry)
	}

	return read
}

func (self responsesDialect) Check(scenario *Scenario, asked Request) string {
	switch {
	case !asked.Streaming:
		return "only streaming responses are supported"
	case asked.Stored:
		return "this endpoint does not store conversations"
	case asked.Instructions == "":
		return "the request carried no instructions"
	case asked.Model != scenario.Model:
		return fmt.Sprintf("the model %q is not available", asked.Model)
	}

	if hanging := unansweredCall(asked.Input); hanging != "" {
		return "No tool output found for function call " + hanging + "."
	}

	return ""
}

func (self responsesDialect) Play(stream *Stream, _ *Scenario, turn Turn) {
	for _, thought := range turn.Think {
		stream.Type(responses.Thought, thought)
		stream.Send(responses.ThinkingPart(thought))
		stream.Send(responses.ReasoningItem(stream.ID("rs"), thought))
	}

	if turn.Say != "" {
		stream.Type(responses.Answer, turn.Say)

		if turn.Truncate {
			return
		}

		stream.Send(responses.Message(turn.Say))
	}

	if turn.Truncate {
		return
	}

	for _, call := range turn.Calls {
		stream.Send(responses.Call(stream.ID("call"), call.Name, call.Arguments))
	}

	switch {
	case turn.ErrorEvent != "":
		stream.Send(turn.ErrorEvent)
	case turn.Fail != "":
		stream.Send(responses.FailedResponse(turn.Fail))
	case turn.Incomplete:
		stream.Send(responses.IncompleteResponse)
	default:
		stream.Send(responses.CompletedResponse(freshTokens + cachedTokens))
	}

	stream.Send(responses.Done)
}

func (self responsesDialect) Exhausted(stream *Stream, message string) {
	stream.Send(responses.Answer(message))
	stream.Send(responses.Message(message))
	stream.Send(responses.CompletedResponse(freshTokens + cachedTokens))
	stream.Send(responses.Done)
}
