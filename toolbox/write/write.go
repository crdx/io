package write

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"crdx.org/io/internal/file"
	"crdx.org/io/internal/pathutil"
	"crdx.org/io/internal/util"
	"crdx.org/io/tool"
)

// Args is what a write takes.
type Args struct {
	Path    string `json:"path"`    // the file to write
	Content string `json:"content"` // the full contents
}

// New builds a write tool confined to root.
func New(root *file.Root) tool.Tool {
	return writer{
		Tool: tool.FocusPath(tool.Implement(
			tool.Definition{
				Name:        "write",
				Description: "write a file, creating or overwriting it",
				Schema: tool.Schema{
					tool.String("path", "file"),
					tool.String("content", "full contents"),
				},
			},
			Render,
		).Stats(func(_ context.Context, args Args) (string, tool.Stats, error) { return exec(root, args) })),

		root: root,
	}
}

type writer struct {
	tool.Tool // the write tool itself

	root *file.Root // the tree written to
}

func (self writer) ReadOnly() bool { return self.root.RefuseWrite(".") != nil }

// Render names the file, saying how much is being written rather than what.
func Render(args Args) (string, string) {
	return pathutil.Shorten(args.Path), util.FormatBytes(len(args.Content), 3)
}

func exec(root *file.Root, args Args) (string, tool.Stats, error) {
	if args.Path == "" {
		return "", tool.Stats{}, errors.New("path is required")
	}

	root, name, err := root.Resolve(args.Path)
	if err != nil {
		return "", tool.Stats{}, err
	}

	if err := root.RefuseWrite(name); err != nil {
		return "", tool.Stats{}, err
	}

	if directory := filepath.Dir(name); directory != "." {
		if err := root.MkdirAll(directory, 0o755); err != nil {
			return "", tool.Stats{}, err
		}
	}

	if err := root.WriteFile(name, []byte(args.Content), 0o644); err != nil {
		return "", tool.Stats{}, err
	}

	lines := int64(0)
	if args.Content != "" {
		lines = int64(strings.Count(args.Content, "\n"))
		if !strings.HasSuffix(args.Content, "\n") {
			lines++
		}
	}
	stats := tool.Stats{
		Kind: tool.StatsWrite, Lines: lines, Bytes: int64(len(args.Content)),
	}
	return fmt.Sprintf("wrote %s to %s", util.FormatBytes(len(args.Content), 3), args.Path), stats, nil
}
