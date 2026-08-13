package agent

import "crdx.org/io/tool"

// Provider is a backend a conversation is held with.
type Provider interface {
	Configure(string, []tool.Definition)
	AddUserMessage(string)
	AddToolResults([]ToolResult)
	Send(Yield) (Reply, error)
}

// Yield is handed each fragment of an answer as it arrives, and returns false to end the turn where
// whoever asked has stopped listening.
type Yield func(text string) bool

// Reply is what a turn amounted to once it was over: the calls the model made.
type Reply struct {
	Calls []ToolCall
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

// Kind is what an event is.
type Kind int

// The events a turn is made of: a fragment of the answer, a call the model asked for, and what that
// call handed back.
const (
	Text Kind = iota
	Call
	Result
)

// Event is one thing that happened on the way to an answer.
type Event struct {
	Kind      Kind
	Value     string
	Name      string
	Arguments string
	ID        string
}

// Agent holds a conversation.
type Agent struct {
	provider Provider
	tools    map[string]tool.Tool
}
