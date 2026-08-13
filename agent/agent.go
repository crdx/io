package agent

import (
	"fmt"
	"iter"
	"strings"

	"crdx.org/io/tool"
)

// New builds an agent that talks to a provider with a set of tools on offer.
func New(prompt string, provider Provider, tools []tool.Tool) *Agent {
	definitions := make([]tool.Definition, len(tools))
	available := make(map[string]tool.Tool, len(tools))

	for index, offered := range tools {
		definitions[index] = tool.Describe(offered)
		available[offered.Name()] = offered
	}

	provider.Configure(prompt, definitions)

	return &Agent{provider: provider, tools: available}
}

// Stream answers one prompt, running the model until it stops asking for tools.
func (self *Agent) Stream(prompt string) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		self.provider.AddUserMessage(prompt)

		for {
			listening := true

			reply, err := self.provider.Send(func(text string) bool {
				listening = yield(Event{Kind: Text, Text: text}, nil)
				return listening
			})

			switch {
			case !listening:
				return
			case err != nil:
				yield(Event{}, err)
				return
			case len(reply.Calls) == 0:
				return
			}

			if !self.runCalls(reply.Calls, yield) {
				return
			}
		}
	}
}

// Send answers one prompt the whole way through, and returns everything the model said.
func (self *Agent) Send(prompt string) (string, error) {
	var answer strings.Builder
	var failure error

	for event, err := range self.Stream(prompt) {
		if err != nil {
			failure = err
			break
		}

		if event.Kind == Text {
			answer.WriteString(event.Text)
		}
	}

	return answer.String(), failure
}

func (self *Agent) runCalls(calls []ToolCall, yield func(Event, error) bool) bool {
	results := make([]ToolResult, len(calls))

	for index, call := range calls {
		asked := Event{Kind: Call, Name: call.Name, Arguments: call.Arguments, ID: call.ID}
		if !yield(asked, nil) {
			return false
		}

		output := self.runTool(call)
		results[index] = ToolResult{ID: call.ID, Output: output}

		if !yield(Event{Kind: Result, Name: call.Name, Text: output, ID: call.ID}, nil) {
			return false
		}
	}

	self.provider.AddToolResults(results)

	return true
}

func (self *Agent) runTool(call ToolCall) string {
	called, found := self.tools[call.Name]
	if !found {
		return fmt.Sprintf("there is no tool called %q", call.Name)
	}

	parsed, err := called.Parse(call.Arguments)
	if err != nil {
		return err.Error()
	}

	output, err := parsed.Exec()
	if err != nil {
		return err.Error()
	}

	return output
}
