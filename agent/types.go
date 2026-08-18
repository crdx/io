package agent

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"crdx.org/io/tool"
)

// Provider is a backend a conversation is held with.
type Provider interface {
	Configure(string, []tool.Definition)
	AddUserMessage(string)
	AddToolResults([]ToolResult)
	Send(context.Context, Yield) (Reply, error)
}

var (
	// ErrNoState means the provider cannot carry its conversation out of the process.
	ErrNoState = errors.New("the provider does not expose conversation state")
	// ErrStateReplaced means a provider changed state it had already handed out.
	ErrStateReplaced = errors.New("the provider replaced append-only conversation state")
)

// State is a provider whose conversation can be carried out of the process and back into it. What
// an item holds is the provider's business: it goes out opaque and comes back verbatim. State is
// append-only: every Dump returns the previous result as an unchanged prefix followed by new
// items.
type State interface {
	Dump() []json.RawMessage
	Load([]json.RawMessage)
}

// Yield is handed each piece of a turn as it arrives, and returns false to end the turn. A provider
// reports what it has by kind: what the model is thinking, and what it is saying.
type Yield func(Event) bool

// Reply is a turn once it's over.
type Reply struct {
	Calls []ToolCall // the calls the model made
}

// ToolCall is one call the model made.
type ToolCall struct {
	ID        string // which call
	Name      string // which tool
	Arguments string // what the tool was called with
}

// ToolResult is what one call handed back.
type ToolResult struct {
	ID     string     // which call
	Output string     // what the tool returned
	Image  tool.Image // visual content the tool returned
}

// Kind is what an event is. It is a name rather than a number because a conversation is written
// down as it stands, and renumbering a constant would rewrite the past.
type Kind string

// The kinds of event a conversation is made of.
const (
	Prompt      Kind = "prompt"      // what was asked
	Reasoning   Kind = "reasoning"   // what the model thought on the way to answering
	Text        Kind = "text"        // what was answered
	Call        Kind = "call"        // a tool the model asked for
	Result      Kind = "result"      // what that tool handed back
	Interrupted Kind = "interrupted" // where a replacement prompt stopped a turn
)

// Event is a thing that happened. The stream of them is the conversation itself, so an event is
// written down as it stands: one event, one line.
//
// Name and Arguments are what a call was; Render and Detail are only how it looked at the time,
// kept for a display that no longer has the tool to ask.
type Event struct {
	Kind       Kind             `json:"kind"`                // what happened
	Text       string           `json:"text,omitempty"`      // what was said or thought
	ID         string           `json:"id,omitempty"`        // which call
	Name       string           `json:"name,omitempty"`      // which tool
	Arguments  string           `json:"arguments,omitempty"` // what the tool was called with
	Render     string           `json:"render,omitempty"`    // how the call was shown when it ran
	Detail     string           `json:"detail,omitempty"`    // what qualified that, for a display to set apart
	Focus      string           `json:"focus,omitempty"`     // the part of the rendering set apart from the rest
	Syntax     string           `json:"syntax,omitempty"`    // the language the rendering is written in
	ReadOnly   bool             `json:"read_only,omitempty"` // whether the tool called changes nothing
	Failed     bool             `json:"failed,omitempty"`    // whether a call came back with an error rather than a result
	Took       time.Duration    `json:"took,omitempty"`      // how long a call took to run
	Statistics *tool.Statistics `json:"stats,omitempty"`     // resources or sizes measured by the tool
}

// Agent holds a conversation.
type Agent struct {
	provider Provider             // the conversation backend
	tools    map[string]tool.Tool // the tools by name
	state    []json.RawMessage    // the append-only state already handed out
}
