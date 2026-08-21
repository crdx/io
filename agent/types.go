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
	ID              string   `json:"id"`
	Name            string   `json:"name,omitempty"`
	EffortLevels    []string `json:"efforts,omitempty"`
	MaxOutputTokens int      `json:"output,omitempty"`
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
	Prompt      Kind = "prompt"      // what was asked
	Reasoning   Kind = "reasoning"   // what the model thought on the way to answering
	Text        Kind = "text"        // what was answered
	Call        Kind = "call"        // a tool the model asked for
	Result      Kind = "result"      // what that tool handed back
	StateEvent  Kind = "state"       // durable state changed by a successful call
	Notice      Kind = "notice"      // what the harness said itself, rather than the model
	Startup     Kind = "startup"     // what the harness had ready when the conversation opened
	Interrupted Kind = "interrupted" // where a replacement prompt stopped a turn
	Failure     Kind = "failure"     // why a turn ended before the model completed it
)

// Rendering is how a call looked when it ran. Used if the original tool is unavailable.
type Rendering struct {
	Subject   string         `json:"render,omitempty"`
	Note      string         `json:"detail,omitempty"`
	Highlight tool.Highlight `json:"highlight,omitzero"`
	ReadOnly  bool           `json:"read_only,omitempty"`
}

// Describe takes how a decoded call looks from the call itself, leaving what the call cannot say
// about itself as it was.
func (self *Rendering) Describe(call tool.Call) {
	self.Subject = call.Subject()
	self.Note = call.Qualifier()
	self.Highlight = call.Highlight()
}

// Event is a conversation occurrence or durable tool-state transition.
type Event struct {
	Rendering

	Kind      Kind            `json:"kind"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"` // which tool or state owner
	Arguments string          `json:"arguments,omitempty"`
	Failed    bool            `json:"failed,omitempty"` // whether a call came back with an error rather than a result
	Took      time.Duration   `json:"took,omitempty"`
	Stats     *tool.Stats     `json:"stats,omitempty"`
	State     json.RawMessage `json:"state,omitempty"` // an opaque durable tool-state transition
}

// Agent holds a conversation.
type Agent struct {
	provider    Provider
	tools       map[string]tool.Tool
	stateOwners map[string]tool.Tool // the tools by durable state name
	state       []json.RawMessage    // the append-only provider state already handed out
}
