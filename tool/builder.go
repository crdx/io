package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Builder binds typed describing and validation to a tool definition.
type Builder[T any] struct {
	definition Definition
	describe   Describer[T]
	validate   Validator[T]
}

// Implement binds typed describing to a tool definition.
func Implement[T any](definition Definition, describe Describer[T]) Builder[T] {
	return Builder[T]{
		definition: definition,
		describe:   describe,
	}
}

// Validate returns a builder that validates decoded arguments before constructing a call.
func (self Builder[T]) Validate(validate Validator[T]) Builder[T] {
	self.validate = validate
	return self
}

// Run builds a tool whose calls return one complete result.
func (self Builder[T]) Run(exec ResultExecutor[T]) Tool {
	return self.build(func(args T) Call {
		call := self.call(args)
		call.exec = func(ctx context.Context) (Result, error) {
			return exec(ctx, args)
		}
		return call
	})
}

// Plain builds a tool whose calls return text.
func (self Builder[T]) Plain(exec Executor[T]) Tool {
	return self.Run(func(ctx context.Context, args T) (Result, error) {
		output, err := exec(ctx, args)
		return Result{Output: output}, err
	})
}

// Stats builds a tool whose calls return text and stats.
func (self Builder[T]) Stats(exec StatsExecutor[T]) Tool {
	return self.Run(func(ctx context.Context, args T) (Result, error) {
		output, stats, err := exec(ctx, args)
		return Result{Output: output, Stats: stats}, err
	})
}

// StatsWithImage builds a tool with stats whose calls may also return an image.
func (self Builder[T]) StatsWithImage(exec StatsWithImageExecutor[T]) Tool {
	return self.Run(func(ctx context.Context, args T) (Result, error) {
		output, image, stats, err := exec(ctx, args)
		return Result{Output: output, Image: image, Stats: stats}, err
	})
}

func (self Builder[T]) build(call func(args T) Call) Tool {
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

func (self Builder[T]) call(args T) funcCall {
	return funcCall{
		describe: func() (string, string) { return self.describe(args) },
	}
}
