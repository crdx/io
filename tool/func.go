package tool

import (
	"context"
	"encoding/json"
)

type funcTool struct {
	name        string                               // what the tool is called
	description string                               // what the tool does
	schema      Schema                               // what arguments it takes
	parse       func(arguments string) (Call, error) // decodes one call
}

func (self funcTool) Name() string        { return self.name }
func (self funcTool) Description() string { return self.description }
func (self funcTool) Schema() Schema      { return self.schema }
func (self funcTool) Concurrent() bool    { return false }
func (self funcTool) ReadOnly() bool      { return false }
func (self funcTool) StateKey() string    { return "" }

func (self funcTool) Parse(arguments string) (Call, error) { return self.parse(arguments) }
func (self funcTool) Restore(json.RawMessage) error        { return nil }

type funcCall struct {
	describe func() (string, string)                   // the call's subject and qualifier
	exec     func(ctx context.Context) (Result, error) // runs the call
}

func (self funcCall) Subject() string      { subject, _ := self.describe(); return subject }
func (self funcCall) Qualifier() string    { _, qualifier := self.describe(); return qualifier }
func (self funcCall) Highlight() Highlight { return Highlight{} }

func (self funcCall) Exec(ctx context.Context) (Result, error) { return self.exec(ctx) }
