package agent

import "crdx.org/io/tool"

// Provider is a backend a conversation is held with. The conversation itself belongs to the
// provider, since what a history looks like is the one thing every wire format disagrees about.
type Provider interface {
	Configure(instructions string, tools []tool.Definition)
	AddUserMessage(prompt string)
	AddToolResults(results []ToolResult)
	Send() (Reply, error)
}

// Reply is one response: the calls the model made, and the text it said on the way.
type Reply struct {
	Answer string
	Calls  []ToolCall
}

// ToolCall is one call the model made, in a form no wire format's shape leaks into.
type ToolCall struct {
	ID        string
	Name      string
	Arguments string
}

// ToolResult is what one call handed back, ready for a provider to fold into its own history.
type ToolResult struct {
	ID     string
	Output string
}

// Agent holds a conversation. It draws nothing and reads no prompt.
type Agent struct {
	provider Provider
	tools    map[string]tool.Tool
}
