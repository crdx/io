package web

import (
	"context"
	"errors"

	"crdx.org/io/tool"
)

var ErrAccessWithheld = errors.New("web access is not granted; the user can grant it with ctrl+x s")

func New(isAllowed func() bool, searcher Searcher) []tool.Tool {
	return []tool.Tool{
		newSearch(isAllowed, searcher),
		newFetch(isAllowed, defaultFetchClient()),
	}
}

func requireAccess(isAllowed func() bool) error {
	if isAllowed() {
		return nil
	}

	return ErrAccessWithheld
}

func outputResult(output string) (tool.ToolCallResult, error) {
	if output == "" {
		return tool.ToolCallResult{}, errors.New("the request returned no content")
	}

	return tool.ToolCallResult{Output: output, Stats: tool.OutputStats(output)}, nil
}

func noQualifier[T any](describe func(T) string) tool.Describer[T] {
	return func(args T) (string, string) {
		return describe(args), ""
	}
}

func runAfterAccess[T any](
	isAllowed func() bool,
	execute func(context.Context, T) (tool.ToolCallResult, error),
) tool.ResultExecutor[T] {
	return func(ctx context.Context, args T) (tool.ToolCallResult, error) {
		if err := requireAccess(isAllowed); err != nil {
			return tool.ToolCallResult{}, err
		}

		return execute(ctx, args)
	}
}
