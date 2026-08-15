package agent

import "crdx.org/io/tool"

// Provider is a backend a conversation is held with.
type Provider interface {
	Configure(string, []tool.Definition)
	AddUserMessage(string)
	AddToolResults([]ToolResult)
	Send(Yield) (Reply, error)
}

// Yield is handed each fragment of an answer as it arrives, and returns false to end the turn.
type Yield func(text string) bool

// Reply is a turn once it's over.q
type Reply struct {
	Calls []ToolCall
}

// ToolCall is one call the model made.
type ToolCall struct {
	ID        string
	Name      string
	Arguments string
}

// ToolResult is what one call handed back.
type ToolResult struct {
	ID     string
	Output string
}

// Kind is what an event is.
type Kind int

const (
	Text Kind = iota
	Call
	Result
)

// Event is a thing that happened.
type Event struct {
	Kind      Kind
	Payload   string
	Name      string
	Arguments string
	Render    string
	ID        string
}

// Agent holds a conversation.
type Agent struct {
	provider Provider
	tools    map[string]tool.Tool
}
