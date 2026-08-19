package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Impl binds typed rendering and validation to a tool definition.
type Impl[T any] struct {
	definition Definition
	render     Renderer[T]
	validate   Validator[T]
}

// Implement binds typed rendering to a tool definition.
func Implement[T any](definition Definition, render Renderer[T]) Impl[T] {
	return Impl[T]{definition: definition, render: render}
}

// Validate returns an implementation that validates decoded arguments before constructing a call.
func (self Impl[T]) Validate(validate Validator[T]) Impl[T] {
	self.validate = validate
	return self
}

// Plain builds a tool whose calls return text.
func (self Impl[T]) Plain(exec Executor[T]) Tool {
	return self.build(func(args T) Call {
		call := self.call(args)
		call.exec = func(ctx context.Context) (string, error) {
			return exec(ctx, args)
		}
		return call
	})
}

// Stats builds a tool whose calls return text and stats.
func (self Impl[T]) Stats(exec StatsExecutor[T]) Tool {
	return self.build(func(args T) Call {
		return &statsCall{
			Call: self.call(args),
			exec: func(ctx context.Context) (string, Stats, error) {
				return exec(ctx, args)
			},
		}
	})
}

// StatsWithImage builds a tool with stats whose calls may also return an image.
func (self Impl[T]) StatsWithImage(exec StatsWithImageExecutor[T]) Tool {
	return self.build(func(args T) Call {
		return &statsWithImageCall{
			Call: self.call(args),
			exec: func(ctx context.Context) (string, Image, Stats, error) {
				return exec(ctx, args)
			},
		}
	})
}

func (self Impl[T]) build(call func(args T) Call) Tool {
	return funcTool{
		name:        self.definition.Name,
		description: self.definition.Description,
		schema:      self.definition.Schema,
		parse: func(arguments string) (Call, error) {
			var args T
			if text := strings.TrimSpace(arguments); text != "" {
				if err := json.Unmarshal([]byte(text), &args); err != nil {
					return nil, fmt.Errorf("could not parse the arguments: %w", err)
				}
			}

			if self.validate != nil {
				if err := self.validate(args); err != nil {
					return nil, err
				}
			}

			return call(args), nil
		},
	}
}

func (self Impl[T]) call(args T) funcCall {
	return funcCall{
		render: func() (string, string) { return self.render(args) },
	}
}
