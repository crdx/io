package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"time"
)

// Tool is one operation the model can call.
type Tool interface {
	Name() string
	Description() string
	Schema() Schema
	Concurrent() bool
	ReadOnly() bool
	Parse(arguments string) (Call, error)
}

// Concurrent marks a tool as safe to run alongside others.
func Concurrent(inner Tool) Tool {
	return concurrent{Tool: inner}
}

type concurrent struct {
	Tool // the tool safe to run concurrently
}

func (self concurrent) Concurrent() bool { return true }

// ReadOnly marks a tool as changing nothing.
func ReadOnly(inner Tool) Tool {
	return readOnly{Tool: inner}
}

type readOnly struct {
	Tool // the tool that changes nothing
}

func (self readOnly) ReadOnly() bool { return true }

// Call is one decoded invocation. Render and Detail contain unstyled display text. Exec must pass
// its context to work started outside the process.
type Call interface {
	Render() string
	Detail() string
	Exec(ctx context.Context) (string, error)
}

// Statistics are resources consumed by a call, where its tool can measure them.
type Statistics struct {
	Kind       string        `json:"kind,omitempty"`
	CPUTime    time.Duration `json:"cpu_time,omitempty"`
	PeakMemory uint64        `json:"peak_memory,omitempty"`
	Lines      int64         `json:"lines,omitempty"`
	Bytes      int64         `json:"bytes,omitempty"`
	Added      int64         `json:"added,omitempty"`
	Removed    int64         `json:"removed,omitempty"`
}

// Statistics kinds classify call measurements.
const (
	StatsResources = "resources"
	StatsRead      = "read"
	StatsWrite     = "write"
	StatsDiff      = "diff"
)

// Statistical is a call that records resource use while it runs.
type Statistical interface {
	Call
	Statistics() (Statistics, bool)
}

// Stats returns a call's resource statistics and whether it measured any.
func Stats(call Call) (Statistics, bool) {
	statisticalCall, ok := call.(Statistical)
	if !ok {
		return Statistics{}, false
	}
	return statisticalCall.Statistics()
}

// FocusedCall has one part of its rendering set apart from the rest.
type FocusedCall interface {
	Call
	Focus() string
}

// SyntaxCall has a rendering written in a language a display may highlight.
type SyntaxCall interface {
	Call
	Syntax() string
}

// Syntax marks the language a call rendering is written in.
func Syntax(inner Tool, language string) Tool {
	return syntaxTool{Tool: inner, language: language}
}

type syntaxTool struct {
	Tool

	language string
}

func (self syntaxTool) Parse(arguments string) (Call, error) {
	call, err := self.Tool.Parse(arguments)
	if err != nil {
		return nil, err
	}

	return syntaxCall{Call: call, language: self.language}, nil
}

type syntaxCall struct {
	Call

	language string
}

func (self syntaxCall) Syntax() string { return self.language }

func (self syntaxCall) Statistics() (Statistics, bool) { return Stats(self.Call) }

// Focus marks the part of a call rendering a display should set apart.
func Focus(inner Tool, pick func(Call) string) Tool {
	return focusedTool{Tool: inner, pick: pick}
}

// FocusPath sets apart the last component of a path rendering.
func FocusPath(inner Tool) Tool {
	return Focus(inner, func(call Call) string {
		renderedCall := call.Render()
		if renderedCall == "" {
			return ""
		}

		return path.Base(renderedCall)
	})
}

type focusedTool struct {
	Tool

	pick func(Call) string
}

func (self focusedTool) Parse(arguments string) (Call, error) {
	call, err := self.Tool.Parse(arguments)
	if err != nil {
		return nil, err
	}

	return focusedCall{Call: call, focus: self.pick(call)}, nil
}

type focusedCall struct {
	Call

	focus string
}

func (self focusedCall) Focus() string { return self.focus }

func (self focusedCall) Statistics() (Statistics, bool) { return Stats(self.Call) }

// DefineMeasured builds a tool whose calls report resource statistics.
func DefineMeasured[T any](
	name string,
	description string,
	schema Schema,
	render func(args T) (string, string),
	exec func(ctx context.Context, args T) (string, Statistics, error),
) Tool {
	return funcTool{
		name: name, description: description, schema: schema,
		parse: func(arguments string) (Call, error) {
			var args T
			if s := strings.TrimSpace(arguments); s != "" {
				if err := json.Unmarshal([]byte(s), &args); err != nil {
					return nil, fmt.Errorf("could not parse the arguments: %w", err)
				}
			}

			plain := funcCall{
				render: func() (string, string) { return render(args) },
			}
			return &measuredCall{
				Call: plain,
				exec: func(ctx context.Context) (string, Statistics, error) {
					return exec(ctx, args)
				},
			}, nil
		},
	}
}

// Define builds a tool from functions that render and execute its argument type.
func Define[T any](
	name string,
	description string,
	schema Schema,
	render func(args T) (string, string),
	exec func(ctx context.Context, args T) (string, error),
) Tool {
	return funcTool{
		name:        name,
		description: description,
		schema:      schema,

		parse: func(arguments string) (Call, error) {
			var args T

			if s := strings.TrimSpace(arguments); s != "" {
				if err := json.Unmarshal([]byte(s), &args); err != nil {
					return nil, fmt.Errorf("could not parse the arguments: %w", err)
				}
			}

			return funcCall{
				render: func() (string, string) { return render(args) },
				exec: func(ctx context.Context) (string, error) {
					return exec(ctx, args)
				},
			}, nil
		},
	}
}
