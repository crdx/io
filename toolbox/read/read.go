package read

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"crdx.org/io/internal/file"
	"crdx.org/io/internal/pathutil"
	"crdx.org/io/internal/strutil"
	"crdx.org/io/tool"
)

// Args is what a read takes. An absent offset or limit is zero, which means the whole file.
type Args struct {
	Path   string `json:"path"`   // the file to read
	Offset int    `json:"offset"` // the first line
	Limit  int    `json:"limit"`  // the most lines to return
}

// New builds the read tool confined to root.
func New(root *file.Root) tool.Tool {
	definedTool := tool.DefineMeasured(
		"read",
		"read a file",
		tool.Schema{
			tool.String("path", "file"),
			tool.Integer("offset", "first line, 1-indexed").Optional(),
			tool.Integer("limit", "max lines").Optional(),
		},
		Render,
		func(_ context.Context, args Args) (string, tool.Statistics, error) { return exec(root, args) },
	)

	return tool.ReadOnly(tool.Concurrent(tool.FocusPath(definedTool)))
}

// Render describes a read as the path, qualified by the lines it asks for.
func Render(args Args) (string, string) {
	return pathutil.Shorten(args.Path), span(args.Offset, args.Limit)
}

func span(offset int, limit int) string {
	switch {
	case offset > 0 && limit > 0:
		return fmt.Sprintf("%d-%d", offset, offset+limit-1)
	case offset > 0:
		return fmt.Sprintf("%d+", offset)
	case limit > 0:
		return fmt.Sprintf("1-%d", limit)
	default:
		return ""
	}
}

func exec(root *file.Root, args Args) (string, tool.Statistics, error) {
	if args.Path == "" {
		return "", tool.Statistics{}, errors.New("path is required")
	}

	root, name, err := root.Resolve(args.Path)
	if err != nil {
		return "", tool.Statistics{}, err
	}

	data, err := root.ReadFile(name)
	if err != nil {
		if pathError, ok := errors.AsType[*fs.PathError](err); ok {
			return "", tool.Statistics{}, fmt.Errorf("%s: %w", args.Path, pathError.Err)
		}

		return "", tool.Statistics{}, err
	}

	lines := strutil.Lines(string(data))
	stats := tool.Statistics{
		Kind: tool.StatsRead, Lines: int64(len(lines)), Bytes: int64(len(data)),
	}

	if args.Offset <= 0 && args.Limit <= 0 {
		return string(data), stats, nil
	}

	start := max(args.Offset-1, 0)
	if start >= len(lines) {
		return "", stats, fmt.Errorf(
			"offset %d is past the end of the file (%d lines)", args.Offset, len(lines),
		)
	}

	end := len(lines)
	if args.Limit > 0 && start+args.Limit < end {
		end = start + args.Limit
	}

	output := strings.Join(lines[start:end], "\n")
	stats.Lines = int64(end - start)
	stats.Bytes = int64(len(output))
	return output, stats, nil
}
