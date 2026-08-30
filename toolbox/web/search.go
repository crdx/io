package web

import (
	"context"
	"errors"
	"strings"

	"crdx.org/io/tool"
)

type SearchArgs struct {
	Query string `json:"query"`
}

type Searcher interface {
	Search(context context.Context, query string) (string, error)
}

func newSearch(allowed func() bool, searcher Searcher) tool.Tool {
	return tool.Implement(
		tool.Definition{
			Name:        "web_search",
			Description: "search the web using OpenAI and return a cited answer",
			Schema: tool.Schema{
				tool.String("query", "web search query"),
			},
		},
		noQualifier(func(args SearchArgs) string { return args.Query }),
	).
		Validate(validateSearch).
		IsEmbarrassinglyParallel().
		ChangesNothing().
		Run(runAfterAccess(allowed, func(ctx context.Context, args SearchArgs) (tool.ToolCallResult, error) {
			output, err := searcher.Search(ctx, args.Query)
			if err != nil {
				return tool.ToolCallResult{}, err
			}

			return outputResult(output)
		}))
}

func validateSearch(args SearchArgs) error {
	if strings.TrimSpace(args.Query) == "" {
		return errors.New("query is required")
	}

	return nil
}
