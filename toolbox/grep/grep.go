package grep

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"crdx.org/io/internal/util"
	"crdx.org/io/tool"
)

// Args is what a search by content takes. An absent path is the working directory, and an absent
// glob every file below it.
type Args struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path"`
	Glob    string `json:"glob"`
}

// New builds the grep tool confined to root. A match is reported as path:line:text.
func New(root *os.Root) tool.Tool {
	return tool.Define(
		"grep",
		"search file contents",
		tool.Schema{
			tool.String("pattern", "regexp"),
			tool.String("path", "directory, defaults to working directory").Optional(),
			tool.String("glob", "path filter, e.g. **/*.go").Optional(),
		},
		Render,
		func(args Args) (string, error) { return exec(root, args) },
	)
}

// Render describes the search out loud.
func Render(args Args) string {
	return util.RenderSearch(args.Pattern, args.Path, args.Glob)
}

func exec(root *os.Root, args Args) (string, error) {
	if args.Pattern == "" {
		return "", errors.New("pattern is required")
	}

	expression, err := regexp.Compile(args.Pattern)
	if err != nil {
		return "", fmt.Errorf("invalid pattern: %w", err)
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

		if !withinGlob(args.Glob, name, path) {
			return nil
		}

		data, err := fs.ReadFile(root.FS(), path)
		if err != nil {
			return nil //nolint:nilerr // an unreadable file is skipped, not fatal
		}

		for number, line := range strings.Split(string(data), "\n") {
			if len(matches) >= util.MaxMatches {
				truncated = true

				break
			}

			if expression.MatchString(line) {
				matches = append(matches, fmt.Sprintf("%s:%d:%s", path, number+1, line))
			}
		}

		return nil
	})
	if err != nil {
		return "", err
	}

	return util.Report(matches, truncated), nil
}

func withinGlob(pattern string, root string, path string) bool {
	if pattern == "" {
		return true
	}

	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}

	return util.MatchGlob(pattern, relative)
}
