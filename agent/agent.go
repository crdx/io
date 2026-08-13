package agent

import (
	"fmt"
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

// Send answers one prompt, running the model until it stops asking for tools, and returns what it
// said.
func (self *Agent) Send(prompt string) (string, error) {
	self.provider.AddUserMessage(prompt)

	var answer strings.Builder

	for {
		reply, err := self.provider.Send()
		answer.WriteString(reply.Answer)

		if err != nil {
			return answer.String(), err
		}

		if len(reply.Calls) == 0 {
			return answer.String(), nil
		}

		self.provider.AddToolResults(self.runTools(reply.Calls))
	}
}

func (self *Agent) runTools(calls []ToolCall) []ToolResult {
	results := make([]ToolResult, len(calls))

	for index, call := range calls {
		results[index] = ToolResult{ID: call.ID, Output: self.runTool(call)}
	}

	return results
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
