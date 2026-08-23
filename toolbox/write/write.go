package write

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"crdx.org/io/internal/file"

	"crdx.org/io/internal/util"
	"crdx.org/io/tool"
)

// Args is what a write takes.
type Args struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// New builds a write tool confined to root.
func New(root *file.Root, snapshots *file.Snapshots) tool.Tool {
	return tool.Implement(
		tool.Definition{
			Name:        "write",
			Description: "write a file, creating it or overwriting it (read it first)",
			Schema: tool.Schema{
				tool.String("path", "file"),
				tool.String("content", "full contents"),
			},
		},
		Describe,
	).FocusPath().Stats(func(_ context.Context, args Args) (string, tool.Stats, error) {
		return exec(root, snapshots, args)
	})
}

// Describe names the file, saying how much is being written rather than what.
func Describe(args Args) (string, string) {
	return args.Path, util.FormatBytes(len(args.Content), 3)
}

func exec(root *file.Root, snapshots *file.Snapshots, args Args) (string, tool.Stats, error) {
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

	currentContent, err := root.ReadFile(name)
	if err == nil {
		if err := snapshots.Check(root, name, currentContent); err != nil {
			return "", tool.Stats{}, fmt.Errorf("%w; read %s before overwriting it", err, args.Path)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", tool.Stats{}, err
	}

	if directory := filepath.Dir(name); directory != "." {
		if err := root.MkdirAll(directory, 0o755); err != nil {
			return "", tool.Stats{}, err
		}
	}

	writtenContent := []byte(args.Content)
	if err := root.WriteFile(name, writtenContent, 0o644); err != nil {
		return "", tool.Stats{}, err
	}
	snapshots.Record(root, name, writtenContent)

	lines := int64(0)
	if args.Content != "" {
		lines = int64(strings.Count(args.Content, "\n"))
		if !strings.HasSuffix(args.Content, "\n") {
			lines++
		}
	}
	stats := tool.Stats{
		Kind:  tool.StatsWrite,
		Lines: lines,
		Bytes: int64(len(args.Content)),
	}
	return fmt.Sprintf("wrote %s to %s", util.FormatBytes(len(args.Content), 3), args.Path), stats, nil
}
