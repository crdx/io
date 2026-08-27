package agent

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"crdx.org/io/tool"
)

type Provider interface {
	Configure(systemPrompt string, tools []tool.Definition)
	AddUserMessage(text string)
	AddToolResults(toolResults []ToolCallResult)
	Send(ctx context.Context, yield Yield) (Reply, error)
}

var (
	ErrNoState       = errors.New("the provider does not expose conversation state")
	ErrStateReplaced = errors.New("the provider replaced append-only conversation state")
)

type State interface {
	Dump() []json.RawMessage
	Load([]json.RawMessage)
}

type Model struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name,omitempty"`
	EffortLevels        []string `json:"efforts,omitempty"`
	ContextWindowTokens int      `json:"context,omitempty"`
	MaxOutputTokens     int      `json:"output,omitempty"`
}

type Lister interface {
	Models(context.Context) ([]Model, error)
}

type UsageWindow struct {
	Duration  time.Duration `json:"duration"`
	Percent   float64       `json:"percent"`
	ResetsAt  time.Time     `json:"resets_at"`
	Scope     string        `json:"scope,omitempty"`
	IsLimited bool          `json:"limited,omitempty"`
}

type UsageReporter interface {
	IsAvailable() bool
	UsageWindows(context.Context) ([]UsageWindow, error)
}

type Output struct {
	Kind       Kind
	Text       string
	Done       bool
	AwaitUsage bool
	Usage      *Usage
}

type Yield func(Output) bool

type Delta struct {
	Kind Kind
	Text string
}

type Update struct {
	Delta *Delta
	Event *Event
}

type Reply struct {
	Calls []ToolCall
	Usage Usage
}

type Usage struct {
	InputTokens int `json:"input_tokens"`
}

type ToolCall struct {
	ID        string
	Name      string
	Arguments string
}

type ToolCallResult struct {
	ID     string
	Output string
	Image  tool.Image
}

type Kind string

const (
	StartupEvent         Kind = "startup"
	UserMessageEvent     Kind = "user_message"
	HarnessMessageEvent  Kind = "harness_message"
	ModelReasoningEvent  Kind = "model_reasoning"
	ModelMessageEvent    Kind = "model_message"
	ToolCallRequestEvent Kind = "tool_call_request"
	ToolCallResultEvent  Kind = "tool_call_result"
	StateChangeEvent     Kind = "state_change"
	InterruptionEvent    Kind = "interruption"
	RetryingEvent        Kind = "retrying"
	FailureEvent         Kind = "failure"
)

type Retriable interface {
	error

	Retriable() bool
	RetryAfter() time.Duration
}

type FallbackRendering struct {
	Subject  string        `json:"render,omitempty"`
	Note     string        `json:"detail,omitempty"`
	Emphasis tool.Emphasis `json:"emphasis,omitzero"`
	ReadOnly bool          `json:"read_only,omitempty"`
}

func (self *FallbackRendering) Describe(toolCall tool.ToolCall) {
	self.Subject = toolCall.Subject()
	self.Note = toolCall.Qualifier()
	self.Emphasis = toolCall.Emphasis()
}

type Status string

const (
	InfoStatus    Status = "info"
	SuccessStatus Status = "success"
	WarningStatus Status = "warning"
	ErrorStatus   Status = "error"
)

type Event struct {
	FallbackRendering

	Kind      Kind            `json:"kind"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Arguments string          `json:"arguments,omitempty"`
	Status    Status          `json:"status,omitempty"`
	Took      time.Duration   `json:"took,omitempty"`
	Attempt   int             `json:"attempt,omitempty"`
	Stats     *tool.Stats     `json:"stats,omitempty"`
	State     json.RawMessage `json:"state,omitempty"`
	Usage     *Usage          `json:"usage,omitempty"`
}

type Agent struct {
	provider         Provider
	registeredTools  map[string]tool.Tool
	enabledToolNames map[string]struct{}
	owners           map[string]tool.Tool
	state            []json.RawMessage

	retryWaitsPassAtOnce bool
}
