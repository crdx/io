package sim

import (
	"encoding/json"
	"fmt"
	"net/http"

	"crdx.org/io/internal/sim/wire/completions"
)

type completionsDialect struct{}

func (self completionsDialect) Name() string {
	return Completions
}

func (self completionsDialect) Path() string {
	return "/chat/completions"
}

type completionsBody struct {
	Model    string `json:"model"`
	Stream   bool   `json:"stream"`
	Messages []struct {
		Role       string `json:"role"`
		Content    any    `json:"content"`
		ToolCallID string `json:"tool_call_id"`
		ToolCalls  []struct {
			ID       string `json:"id"`
			Function struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			} `json:"function"`
		} `json:"tool_calls"`
	} `json:"messages"`

	Tools []struct {
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	} `json:"tools"`
}

func (self completionsDialect) Read(_ *http.Request, raw []byte) (Request, bool) {
	var sent completionsBody
	if json.Unmarshal(raw, &sent) != nil {
		return Request{}, false
	}

	asked := Request{
		API:       self.Name(),
		Model:     sent.Model,
		Streaming: sent.Stream,
	}

	var roles []string

	for i, message := range sent.Messages {
		if message.Role == "system" && i == 0 {
			asked.Instructions = flatten(message.Content)

			continue
		}

		roles = append(roles, message.Role)

		item, _ := json.Marshal(message) //nolint:errchkjson // read from JSON a moment ago

		entry := Entry{
			Type:    Message,
			Role:    message.Role,
			Content: flatten(message.Content),
			CallID:  message.ToolCallID,
			Raw:     item,
		}

		if message.Role == "tool" {
			entry.Type = CallOutput
			entry.Output = entry.Content
		}

		asked.Input = append(asked.Input, entry)

		for _, call := range message.ToolCalls {
			asked.Input = append(asked.Input, Entry{
				Type:      CallMade,
				CallID:    call.ID,
				Name:      call.Function.Name,
				Arguments: call.Function.Arguments,
				Raw:       item,
			})
		}
	}

	asked.Turn = assistantTurns(roles)

	for _, offered := range sent.Tools {
		asked.Tools = append(asked.Tools, offered.Function.Name)
	}

	return asked, true
}

func (self completionsDialect) Check(scenario *Scenario, asked Request) string {
	switch {
	case !asked.Streaming:
		return "only streaming responses are supported"
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

func (self completionsDialect) Play(stream *Stream, _ *Scenario, turn Turn) {
	for _, thought := range turn.Think {
		stream.Type(completions.Thought, thought)
	}

	if turn.Truncate {
		return
	}

	if turn.Say != "" {
		stream.Type(completions.Answer, turn.Say)
	}

	for i, call := range turn.Calls {
		stream.Send(completions.Call(i, stream.ID("call"), call.Name, call.Arguments))
	}

	switch {
	case turn.ErrorEvent != "":
		stream.Send(turn.ErrorEvent)
	case turn.Fail != "":
		stream.Send(completions.Error(turn.Fail))
	case turn.Incomplete:
		stream.Send(completions.Finish(completions.OutOfRoom))
	case len(turn.Calls) > 0:
		stream.Send(completions.Finish(completions.AskedTools))
	default:
		stream.Send(completions.Finish(completions.Stopped))
	}

	stream.Send(completions.Done)
}

func (self completionsDialect) Exhausted(stream *Stream, message string) {
	stream.Send(completions.Answer(message))
	stream.Send(completions.Finish(completions.Stopped))
	stream.Send(completions.Done)
}
