package find

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"crdx.org/io/internal/util"
	"crdx.org/io/tool"
)

// Args is what a search by name takes. An absent path is the working directory.
type Args struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path"`
}

// New builds the find tool confined to root.
func New(root *os.Root) tool.Tool {
	return tool.Define(
		"find",
		"find files",
		tool.Schema{
			tool.String("pattern", "glob"),
			tool.String(
				"path",
				"directory, defaults to working directory",
			).Optional(),
		},
		Render,
		func(args Args) (string, error) { return exec(root, args) },
	)
}

// Render describes the search out loud.
func Render(args Args) string {
	return util.RenderSearch(args.Pattern, args.Path, "")
}

func exec(root *os.Root, args Args) (string, error) {
	if args.Pattern == "" {
		return "", errors.New("pattern is required")
	}

	name, err := util.RootName(root, args.Path)
	if err != nil {
		return "", err
	}

	var matches []string
	truncated := false

	err = util.Walk(root.FS(), name, func(path string, entry fs.DirEntry) error {
		if entry.IsDir() {
			return nil
		}

		if len(matches) >= util.MaxMatches {
			truncated = true

			return fs.SkipAll
		}

		relativePath, err := filepath.Rel(name, path)
		if err != nil {
			return nil //nolint:nilerr // an unrelatable path is skipped, not fatal
		}

		if util.MatchGlob(args.Pattern, relativePath) {
			matches = append(matches, path)
		}

		return nil
	})
	if err != nil {
		return "", err
	}

	return util.Report(matches, truncated), nil
}
