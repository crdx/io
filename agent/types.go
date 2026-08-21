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
	Configure(systemPrompt string, tools []tool.Definition)
	AddUserMessage(text string)
	AddToolResults(toolResults []ToolResult)
	Send(ctx context.Context, yield Yield) (Reply, error)
}

var (
	// ErrNoState means the provider cannot carry its conversation out of the process.
	ErrNoState = errors.New("the provider does not expose conversation state")
	// ErrStateReplaced means a provider changed state it had already handed out.
	ErrStateReplaced = errors.New("the provider replaced append-only conversation state")
)

// State is a provider whose conversation can be carried out of the process and back into it.
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
	Startup         Kind = "startup"           // what the harness had ready when the conversation opened
	UserMessage     Kind = "user_message"      // what was asked
	HarnessMessage  Kind = "harness_message"   // what the harness said itself, rather than the model
	ModelReasoning  Kind = "model_reasoning"   // what the model thought on the way to answering
	ModelMessage    Kind = "model_message"     // what the model answered
	ToolCallRequest Kind = "tool_call_request" // a tool the model asked for
	ToolCallResult  Kind = "tool_call_result"  // what that tool handed back
	StateChange     Kind = "state_change"      // durable state changed by a successful call
	Interruption    Kind = "interruption"      // where a replacement message stopped a turn
	Failure         Kind = "failure"           // why a turn ended before the model completed it
)

// Rendering is a stored version of how a tool call rendered when it ran. Used if the original tool
// is unavailable.
//
// Subject and Note lie along the call's line, left to right, after the name of the tool that the
// event carries rather than this struct:
//
//	read cmd/oh/line/render.go in the workspace
//	└┬─┘ └─────────┬─────────┘ └──────┬───────┘
//	 │             │                  └─ Note, what qualifies the subject
//	 │             └─ Subject, what the call is about
//	 └─ Event.Name, which this struct does not hold
//
// The other two say how that line is drawn rather than what it says: Highlight accents a substring,
// and ReadOnly colours the name.
type Rendering struct {
	Subject   string         `json:"render,omitempty"`
	Note      string         `json:"detail,omitempty"`
	Highlight tool.Highlight `json:"highlight,omitzero"`
	ReadOnly  bool           `json:"read_only,omitempty"`
}

// Describe takes how a decoded call looks from the call itself.
func (self *Rendering) Describe(toolCall tool.Call) {
	self.Subject = toolCall.Subject()
	self.Note = toolCall.Qualifier()
	self.Highlight = toolCall.Highlight()
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
	provider Provider
	tools    map[string]tool.Tool
	owners   map[string]tool.Tool
	state    []json.RawMessage
}
