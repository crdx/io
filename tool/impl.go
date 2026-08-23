package tool

import (
	"context"
	"encoding/json"
)

type _tool struct {
	name        string
	description string
	schema      Schema
	parse       func(arguments string) (_call, error)

	parallel  bool
	readOnly  bool
	stateName string
	restore   Restorer
	highlight func(call ToolCall) Highlight
}

func (self _tool) Name() string        { return self.name }
func (self _tool) Description() string { return self.description }
func (self _tool) Schema() Schema      { return self.schema }
func (self _tool) Concurrent() bool    { return self.parallel }
func (self _tool) ReadOnly() bool      { return self.readOnly }
func (self _tool) StateKey() string    { return self.stateName }

func (self _tool) Restore(state json.RawMessage) error {
	if self.restore == nil {
		return nil
	}

	return self.restore(state)
}

func (self _tool) Parse(arguments string) (ToolCall, error) {
	call, err := self.parse(arguments)
	if err != nil {
		return nil, err
	}
	if self.highlight != nil {
		call.highlight = self.highlight(call)
	}

	return call, nil
}

type _call struct {
	subject   string
	qualifier string
	highlight Highlight
	exec      func(ctx context.Context) (ToolCallResult, error)
}

func (self _call) Subject() string      { return self.subject }
func (self _call) Qualifier() string    { return self.qualifier }
func (self _call) Highlight() Highlight { return self.highlight }

func (self _call) Exec(ctx context.Context) (ToolCallResult, error) { return self.exec(ctx) }
