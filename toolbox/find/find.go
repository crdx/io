package find

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"

	"crdx.org/io/internal/file"
	"crdx.org/io/internal/util"
	"crdx.org/io/tool"
)

// Args is what a search by name takes. An absent path is the working directory.
type Args struct {
	Pattern string `json:"pattern"` // the name glob
	Path    string `json:"path"`    // where to search
}

// New builds the find tool confined to root.
func New(root *file.Root) tool.Tool {
	definedTool := tool.Implement(
		tool.Definition{
			Name:        "find",
			Description: "find files",
			Schema: tool.Schema{
				tool.String("pattern", "glob"),
				tool.String(
					"path",
					"directory, defaults to working directory",
				).Optional(),
			},
		},
		Render,
	).Stats(func(_ context.Context, args Args) (string, tool.Stats, error) {
		return exec(root, args)
	})

	return tool.ReadOnly(tool.Concurrent(tool.Focus(definedTool, util.SearchPath)))
}

// Render describes the search out loud.
func Render(args Args) (string, string) {
	return util.RenderSearch(args.Pattern, args.Path, "")
}

func exec(root *file.Root, args Args) (string, tool.Stats, error) {
	if args.Pattern == "" {
		return "", tool.Stats{}, errors.New("pattern is required")
	}

	root, name, err := root.Resolve(args.Path)
	if err != nil {
		return "", tool.Stats{}, err
	}

	var matches []string
	returnedBytes := int64(0)
	truncated := false

	err = util.Walk(root.FS(), name, func(path string, entry fs.DirEntry) error {
		if entry.IsDir() {
			return nil
		}

		relativePath, err := filepath.Rel(name, path)
		if err != nil {
			return nil //nolint:nilerr // an unrelatable path is skipped, not fatal
		}

		if !util.MatchGlob(args.Pattern, relativePath) {
			return nil
		}

		matches, returnedBytes, truncated = util.AppendSearchResult(matches, returnedBytes, path)
		if truncated {
			return fs.SkipAll
		}

		return nil
	})
	if err != nil {
		return "", tool.Stats{}, err
	}

	output := util.ReportSearchResults(matches, truncated)
	stats := tool.OutputStats(output)
	stats.Truncated = truncated

	return output, stats, nil
}
