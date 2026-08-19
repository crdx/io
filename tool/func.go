package tool

import "context"

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

func (self funcTool) Parse(arguments string) (Call, error) { return self.parse(arguments) }

type funcCall struct {
	render func() (string, string)                   // describes the call
	exec   func(ctx context.Context) (Result, error) // runs the call
}

func (self funcCall) Render() string       { text, _ := self.render(); return text }
func (self funcCall) Detail() string       { _, detail := self.render(); return detail }
func (self funcCall) Highlight() Highlight { return Highlight{} }

func (self funcCall) Exec(ctx context.Context) (Result, error) { return self.exec(ctx) }
