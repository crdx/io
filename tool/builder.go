package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"
)

type Builder[T any] struct {
	definition Definition
	describe   Describer[T]
	validate   Validator[T]

	parallel       bool
	readOnly       bool
	stateName      string
	restore        Restorer
	emphasis       func(call ToolCall) Emphasis
	emphasisSource func(args T, subject string) string
}

func Implement[T any](definition Definition, describe Describer[T]) Builder[T] {
	return Builder[T]{
		definition: definition,
		describe:   describe,
	}
}

func (self Builder[T]) Validate(validate Validator[T]) Builder[T] {
	self.validate = validate
	return self
}

func (self Builder[T]) IsEmbarrassinglyParallel() Builder[T] {
	self.parallel = true
	return self
}

func (self Builder[T]) ChangesNothing() Builder[T] {
	self.readOnly = true
	return self
}

func (self Builder[T]) State(name string, restore Restorer) Builder[T] {
	self.stateName = name
	self.restore = restore

	return self
}

func (self Builder[T]) Syntax(language string) Builder[T] {
	self.emphasis = func(ToolCall) Emphasis {
		return Emphasis{Kind: EmphasisSyntax, Value: language}
	}
	self.emphasisSource = nil

	return self
}

func (self Builder[T]) SyntaxFrom(language string, source func(args T, subject string) string) Builder[T] {
	self = self.Syntax(language)
	self.emphasisSource = source
	return self
}

func (self Builder[T]) Focuses(pick func(ToolCall) string) Builder[T] {
	self.emphasis = func(call ToolCall) Emphasis {
		return Emphasis{Kind: EmphasisFocus, Value: pick(call)}
	}
	self.emphasisSource = nil

	return self
}

func (self Builder[T]) FocusPath() Builder[T] {
	return self.Focuses(func(call ToolCall) string {
		subject := call.Subject()
		if subject == "" {
			return ""
		}

		return path.Base(subject)
	})
}

func (self Builder[T]) Run(execute ResultExecutor[T]) Tool {
	return self.build(execute)
}

func (self Builder[T]) Plain(execute Executor[T]) Tool {
	return self.Run(func(ctx context.Context, args T) (ToolCallResult, error) {
		output, err := execute(ctx, args)
		return ToolCallResult{Output: output}, err
	})
}

func (self Builder[T]) Stats(execute StatsExecutor[T]) Tool {
	return self.Run(func(ctx context.Context, args T) (ToolCallResult, error) {
		output, stats, err := execute(ctx, args)
		return ToolCallResult{Output: output, Stats: stats}, err
	})
}

func (self Builder[T]) StatsWithImage(execute StatsWithImageExecutor[T]) Tool {
	return self.Run(func(ctx context.Context, args T) (ToolCallResult, error) {
		output, image, stats, err := execute(ctx, args)
		return ToolCallResult{Output: output, Image: image, Stats: stats}, err
	})
}

func (self Builder[T]) build(exec ResultExecutor[T]) Tool {
	return _tool{
		name:        self.definition.Name,
		description: self.definition.Description,
		schema:      self.definition.Schema,
		parallel:    self.parallel,
		readOnly:    self.readOnly,
		stateName:   self.stateName,
		restore:     self.restore,
		emphasis:    self.emphasis,
		parse: func(arguments string) (_call, error) {
			var args T
			if text := strings.TrimSpace(arguments); text != "" {
				if err := json.Unmarshal([]byte(text), &args); err != nil {
					return _call{}, fmt.Errorf("could not parse the arguments: %w", err)
				}
			}

			if self.validate != nil {
				if err := self.validate(args); err != nil {
					return _call{}, err
				}
			}

			subject, qualifier := self.describe(args)
			emphasisSource := ""
			if self.emphasisSource != nil {
				emphasisSource = self.emphasisSource(args, subject)
			}
			return _call{
				subject:        subject,
				qualifier:      qualifier,
				emphasisSource: emphasisSource,
				exec: func(ctx context.Context) (ToolCallResult, error) {
					return exec(ctx, args)
				},
			}, nil
		},
	}
}
