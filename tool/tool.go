package tool

import (
	"context"
	"encoding/json"
	"time"

	"crdx.org/io/internal/util/strutil"
)

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

type ToolCall interface {
	Subject() string
	Qualifier() string
	Emphasis() Emphasis
	Exec(ctx context.Context) (ToolCallResult, error)
}

type ToolCallResult struct {
	Output string
	Image  Image
	Stats  Stats
	State  json.RawMessage
}

type Stats struct {
	Kind            string        `json:"kind,omitempty"`
	CPUTime         time.Duration `json:"cpu_time,omitempty"`
	PeakMemory      uint64        `json:"peak_memory,omitempty"`
	Lines           int64         `json:"lines,omitempty"`
	Bytes           int64         `json:"bytes,omitempty"`
	TotalBytes      int64         `json:"total_bytes,omitempty"`
	EstimatedTokens int64         `json:"estimated_tokens,omitempty"`
	AddedLines      int64         `json:"added,omitempty"`
	RemovedLines    int64         `json:"removed,omitempty"`
	IsTruncated     bool          `json:"truncated,omitempty"`
}

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

func OutputStats(output string) Stats {
	bytes := int64(len(output))

	return Stats{
		Kind:       StatsOutput,
		Lines:      int64(len(strutil.Lines(output))),
		Bytes:      bytes,
		TotalBytes: bytes,
	}
}

type EmphasisKind string

const (
	EmphasisSyntax EmphasisKind = "syntax"
	EmphasisFocus  EmphasisKind = "focus"
)

type Emphasis struct {
	Kind   EmphasisKind `json:"kind"`
	Value  string       `json:"value"`
	Source string       `json:"-"`
}

type Describer[T any] func(args T) (string, string)

type Validator[T any] func(args T) error

type ResultExecutor[T any] func(ctx context.Context, args T) (ToolCallResult, error)

type Restorer func(state json.RawMessage) error

type Executor[T any] func(ctx context.Context, args T) (string, error)

type StatsExecutor[T any] func(ctx context.Context, args T) (string, Stats, error)

type StatsWithImageExecutor[T any] func(ctx context.Context, args T) (string, Image, Stats, error)
