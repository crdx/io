package tool

type funcTool struct {
	name        string
	description string
	schema      Schema
	parse       func(arguments string) (Call, error)
}

func (self funcTool) Name() string        { return self.name }
func (self funcTool) Description() string { return self.description }
func (self funcTool) Schema() Schema      { return self.schema }

func (self funcTool) Parse(arguments string) (Call, error) { return self.parse(arguments) }

type funcCall struct {
	render func() string
	exec   func() (string, error)
}

func (self funcCall) Render() string        { return self.render() }
func (self funcCall) Exec() (string, error) { return self.exec() }
