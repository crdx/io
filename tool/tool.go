package tool

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Tool is one thing the model can call.
type Tool interface {
	Name() string
	Description() string
	Schema() Schema
	Parse(arguments string) (Call, error)
}

// Call is one invocation with its arguments already decoded.
type Call interface {
	Render() string
	Exec() (string, error)
}

// Define builds a tool from a pair of functions over its argument type.
func Define[T any](
	name string,
	description string,
	schema Schema,
	render func(args T) string,
	exec func(args T) (string, error),
) Tool {
	return _tool{
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

			return _call{
				render: func() string { return render(args) },
				exec:   func() (string, error) { return exec(args) },
			}, nil
		},
	}
}

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
