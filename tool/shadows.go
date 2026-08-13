package tool

type _tool struct {
	name        string
	description string
	schema      Schema
	parse       func(arguments string) (Call, error)
}

func (self _tool) Name() string        { return self.name }
func (self _tool) Description() string { return self.description }
func (self _tool) Schema() Schema      { return self.schema }

func (self _tool) Parse(arguments string) (Call, error) { return self.parse(arguments) }

type _call struct {
	render func() string
	exec   func() (string, error)
}

func (self _call) Render() string        { return self.render() }
func (self _call) Exec() (string, error) { return self.exec() }
