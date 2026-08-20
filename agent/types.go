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

// Model is one model a provider offers.
type Model struct {
	ID      string   `json:"id"`
	Name    string   `json:"name,omitempty"`
	Efforts []string `json:"efforts,omitempty"`
	Context int      `json:"context,omitempty"`
	Output  int      `json:"output,omitempty"`
}

// Lister is a provider that can say which models it offers.
type Lister interface {
	Models(context.Context) ([]Model, error)
}

// Yield is handed each piece of a turn as it arrives, and returns false to end the turn.
type Yield func(Event) bool

// Reply is a turn once it's over.
type Reply struct {
	Calls []ToolCall
	Usage Usage
}

// Usage is what a completed provider request consumed.
type Usage struct {
	InputTokens int `json:"input_tokens"` // tokens in the context sent to the model
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
	Image  tool.Image
}

// Kind is what an event is. It is a name rather than a number because a conversation is written
// down as it stands, and renumbering a constant would rewrite the past.
type Kind string

// The kinds of event a conversation is made of.
const (
	Prompt       Kind = "prompt"      // what was asked
	Reasoning    Kind = "reasoning"   // what the model thought on the way to answering
	Text         Kind = "text"        // what was answered
	Call         Kind = "call"        // a tool the model asked for
	Result       Kind = "result"      // what that tool handed back
	StateEvent   Kind = "state"       // durable state changed by a successful call
	ContextUsage Kind = "usage"       // how much context a completed turn used
	Interrupted  Kind = "interrupted" // where a replacement prompt stopped a turn
	Failure      Kind = "failure"     // why a turn ended before the model completed it
)

// Event is a conversation occurrence or durable tool-state transition. The stream of them is the
// resumable session itself, so an event is written down as it stands: one event, one line.
//
// Name and Arguments are what a call was; Subject and Qualifier are only how it looked at the time,
// kept for a display that no longer has the tool to ask. The JSON keys stay render and detail so
// sessions written before the rename still read.
type Event struct {
	Kind      Kind            `json:"kind"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"` // which tool or state owner
	Arguments string          `json:"arguments,omitempty"`
	Subject   string          `json:"render,omitempty"` // how the call was shown when it ran
	Qualifier string          `json:"detail,omitempty"` // what qualified that, for a display to set apart
	Highlight tool.Highlight  `json:"highlight,omitzero"`
	ReadOnly  bool            `json:"read_only,omitempty"`
	Failed    bool            `json:"failed,omitempty"` // whether a call came back with an error rather than a result
	Took      time.Duration   `json:"took,omitempty"`
	Stats     *tool.Stats     `json:"stats,omitempty"`
	State     json.RawMessage `json:"state,omitempty"` // an opaque durable tool-state transition
	Usage     *Usage          `json:"usage,omitempty"`
}

// Agent holds a conversation.
type Agent struct {
	provider    Provider
	tools       map[string]tool.Tool
	stateOwners map[string]tool.Tool // the tools by durable state name
	state       []json.RawMessage    // the append-only provider state already handed out
}
