// Package tool defines a tool, its schema, and the wrappers that decorate one.
package tool

import (
	"context"
	"encoding/json"
	"time"

	"crdx.org/io/internal/strutil"
)

// Tool is one operation the model can call.
type Tool interface {
	Name() string
	Description() string
	Schema() Schema
	Concurrent() bool
	ReadOnly() bool
	StateKey() string
	Parse(arguments string) (ToolCall, error)
	Restore(state json.RawMessage) error
}

// ToolCall is one decoded invocation. Subject and Qualifier contain unstyled display text. Exec
// must pass its context to work started outside the process.
type ToolCall interface {
	Subject() string
	Qualifier() string
	Highlight() Highlight
	Exec(ctx context.Context) (ToolCallResult, error)
}

// ToolCallResult is everything a completed call hands back or records for restoration.
type ToolCallResult struct {
	Output string
	Image  Image
	Stats  Stats           // output, change, or resource measurements
	State  json.RawMessage // an opaque durable transition the agent applies and journals
}

// Stats describe a call's output, changes, or measured resource use.
type Stats struct {
	Kind            string        `json:"kind,omitempty"`
	CPUTime         time.Duration `json:"cpu_time,omitempty"`
	PeakMemory      uint64        `json:"peak_memory,omitempty"`
	Lines           int64         `json:"lines,omitempty"`
	Bytes           int64         `json:"bytes,omitempty"`
	TotalBytes      int64         `json:"total_bytes,omitempty"`
	EstimatedTokens int64         `json:"estimated_tokens,omitempty"`
	Added           int64         `json:"added,omitempty"`
	Removed         int64         `json:"removed,omitempty"`
	Truncated       bool          `json:"truncated,omitempty"`
}

// Stats kinds classify call measurements.
const (
	StatsOutput    = "output"
	StatsResources = "resources"
	StatsRead      = "read"
	StatsList      = "list"
	StatsImage     = "image"
	StatsWrite     = "write"
	StatsDiff      = "diff"
	StatsSearch    = "search"
)

// OutputStats describes the complete output returned by a call.
func OutputStats(output string) Stats {
	bytes := int64(len(output))

	return Stats{
		Kind:       StatsOutput,
		Lines:      int64(len(strutil.Lines(output))),
		Bytes:      bytes,
		TotalBytes: bytes,
	}
}

// HighlightKind identifies how a display should highlight a call's rendering.
type HighlightKind string

// The ways a call rendering may be highlighted.
const (
	HighlightSyntax HighlightKind = "syntax"
	HighlightFocus  HighlightKind = "focus"
)

// Highlight describes how a display should highlight a call's rendering.
type Highlight struct {
	Kind  HighlightKind `json:"kind"`
	Value string        `json:"value"`
}

// Describer reports a call's subject and qualifier from decoded arguments.
type Describer[T any] func(args T) (string, string)

// Validator checks decoded tool arguments before a call is constructed.
type Validator[T any] func(args T) error

// ResultExecutor runs a tool call.
type ResultExecutor[T any] func(ctx context.Context, args T) (ToolCallResult, error)

// Restorer applies one durable state transition from a stored session.
type Restorer func(state json.RawMessage) error

// Executor runs a plain tool call.
type Executor[T any] func(ctx context.Context, args T) (string, error)

// StatsExecutor runs a tool call and reports stats.
type StatsExecutor[T any] func(ctx context.Context, args T) (string, Stats, error)

// StatsWithImageExecutor runs a tool call with stats that may return an image.
type StatsWithImageExecutor[T any] func(ctx context.Context, args T) (string, Image, Stats, error)
