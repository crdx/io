package agent

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"crdx.org/io/tool"
)

// —————————————————————————————————————————————————————————————————————————————————————————————————
// mega:allow-file comment-inlines
// —————————————————————————————————————————————————————————————————————————————————————————————————

// Provider is a backend a conversation is held with.
type Provider interface {
	Configure(systemPrompt string, tools []tool.Definition)
	AddUserMessage(text string)
	AddToolResults(toolResults []ToolCallResult)
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
	ID                  string   `json:"id"`
	Name                string   `json:"name,omitempty"`
	EffortLevels        []string `json:"efforts,omitempty"`
	ContextWindowTokens int      `json:"context,omitempty"`
	MaxOutputTokens     int      `json:"output,omitempty"`
}

// Lister is a provider that can say which models it offers.
type Lister interface {
	Models(context.Context) ([]Model, error)
}

// Output is one provider prose fragment or completion boundary.
type Output struct {
	Kind       Kind
	Text       string
	Done       bool
	AwaitUsage bool
	Usage      *Usage
}

// Yield is handed each piece of provider output as it arrives, and returns false to end the turn.
type Yield func(Output) bool

// Delta is provisional model prose arriving during a live stream.
type Delta struct {
	Kind Kind
	Text string
}

// Update is one live-stream delivery. Exactly one of Delta and Event is set.
type Update struct {
	Delta *Delta
	Event *Event
}

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

// ToolCallResult is what one call handed back.
type ToolCallResult struct {
	ID     string
	Output string
	Image  tool.Image
}

// Kind is what an event is. It is a name rather than a number because a conversation is written
// down as it stands, and renumbering a constant would rewrite the past.
type Kind string

// The kinds of event a conversation is made of.
const (
	StartupEvent         Kind = "startup"           // what the harness had ready when the conversation opened
	UserMessageEvent     Kind = "user_message"      // what was asked
	HarnessMessageEvent  Kind = "harness_message"   // what the harness said itself, rather than the model
	ModelReasoningEvent  Kind = "model_reasoning"   // what the model thought on the way to answering
	ModelMessageEvent    Kind = "model_message"     // what the model answered
	ToolCallRequestEvent Kind = "tool_call_request" // a tool the model asked for
	ToolCallResultEvent  Kind = "tool_call_result"  // what that tool handed back
	StateChangeEvent     Kind = "state_change"      // durable state changed by a successful call
	InterruptionEvent    Kind = "interruption"      // where a replacement message stopped a turn
	FailureEvent         Kind = "failure"           // why a turn ended before the model completed it
)

// FallbackRendering is a stored version of how a tool call rendered when it ran. Used if the
// original tool is unavailable.
//
// Subject and Note lie along the call's line, left to right, after the name of the tool that the
// event carries rather than this struct:
//
//	read cmd/oh/edit/render.go in the workspace
//	└┬─┘ └─────────┬─────────┘ └──────┬───────┘
//	 │             │                  └─ Note, what qualifies the subject
//	 │             └─ Subject, what the call is about
//	 └─ Event.Name, which this struct does not hold
//
// The other two say how that line is drawn rather than what it says: Emphasis accents a substring,
// and ReadOnly colours the name.
type FallbackRendering struct {
	Subject  string        `json:"render,omitempty"`
	Note     string        `json:"detail,omitempty"`
	Emphasis tool.Emphasis `json:"emphasis,omitzero"`
	ReadOnly bool          `json:"read_only,omitempty"`
}

// Describe takes how a decoded call looks from the call itself.
func (self *FallbackRendering) Describe(toolCall tool.ToolCall) {
	self.Subject = toolCall.Subject()
	self.Note = toolCall.Qualifier()
	self.Emphasis = toolCall.Emphasis()
}

// Event is a conversation occurrence or durable tool-state transition.
type Event struct {
	FallbackRendering

	Kind      Kind            `json:"kind"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"` // which tool or state owner
	Arguments string          `json:"arguments,omitempty"`
	Failed    bool            `json:"failed,omitempty"` // whether a call came back with an error rather than a result
	Took      time.Duration   `json:"took,omitempty"`
	Stats     *tool.Stats     `json:"stats,omitempty"`
	State     json.RawMessage `json:"state,omitempty"` // an opaque durable tool-state transition
	Usage     *Usage          `json:"usage,omitempty"` // the request usage reported with this response event
}

// Agent holds a conversation.
type Agent struct {
	provider Provider
	tools    map[string]tool.Tool
	owners   map[string]tool.Tool
	state    []json.RawMessage
}
