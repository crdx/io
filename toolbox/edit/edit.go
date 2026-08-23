package edit

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"crdx.org/io/internal/file"

	"crdx.org/io/internal/strutil"
	"crdx.org/io/tool"
)

// Args is what an edit takes.
type Args struct {
	Path    string `json:"path"`
	OldText string `json:"old_text"`
	NewText string `json:"new_text"`
}

// New builds an edit tool confined to root.
func New(root *file.Root, snapshots *file.Snapshots) tool.Tool {
	return tool.Implement(
		tool.Definition{
			Name:        "edit",
			Description: "replace an exact string in a file, which must be read first and appear exactly once",
			Schema: tool.Schema{
				tool.String("path", "file"),
				tool.String("old_text", "exact text to replace, including whitespace"),
				tool.String("new_text", "replacement text"),
			},
		},
		Describe,
	).FocusPath().Stats(func(_ context.Context, args Args) (string, tool.Stats, error) {
		return exec(root, snapshots, args)
	})
}

// Describe names the file.
func Describe(args Args) (string, string) {
	return args.Path, ""
}

func exec(root *file.Root, snapshots *file.Snapshots, args Args) (string, tool.Stats, error) {
	switch {
	case args.Path == "":
		return "", tool.Stats{}, errors.New("path is required")
	case args.OldText == "":
		return "", tool.Stats{}, errors.New("old_text is required")
	}

	root, name, err := root.Resolve(args.Path)
	if err != nil {
		return "", tool.Stats{}, err
	}

	if err := root.RefuseWrite(name); err != nil {
		return "", tool.Stats{}, err
	}

	data, err := root.ReadFile(name)
	if err != nil {
		return "", tool.Stats{}, err
	}
	if err := snapshots.Check(root, name, data); err != nil {
		return "", tool.Stats{}, fmt.Errorf("%w — ensure you read %s before editing it", err, args.Path)
	}

	content := string(data)

	switch strings.Count(content, args.OldText) {
	case 0:
		return "", tool.Stats{}, errors.New("old_text does not appear in the file")
	case 1:
	default:
		return "", tool.Stats{}, errors.New(
			"old_text appears more than once — include more context to disambiguate",
		)
	}

	info, err := root.Stat(name)
	if err != nil {
		return "", tool.Stats{}, err
	}

	updatedContent := strings.Replace(content, args.OldText, args.NewText, 1)

	updatedData := []byte(updatedContent)
	if err := root.WriteFile(name, updatedData, info.Mode()); err != nil {
		return "", tool.Stats{}, err
	}
	snapshots.Record(root, name, updatedData)

	addedLines, removedLines := changedLines(args.OldText, args.NewText)
	stats := tool.Stats{Kind: tool.StatsDiff, Added: addedLines, Removed: removedLines}
	return "edited " + args.Path, stats, nil
}

func changedLines(before string, after string) (int64, int64) {
	oldLines := strutil.Lines(before)
	newLines := strutil.Lines(after)

	for len(oldLines) > 0 && len(newLines) > 0 && oldLines[0] == newLines[0] {
		oldLines, newLines = oldLines[1:], newLines[1:]
	}
	for len(oldLines) > 0 && len(newLines) > 0 &&
		oldLines[len(oldLines)-1] == newLines[len(newLines)-1] {
		oldLines, newLines = oldLines[:len(oldLines)-1], newLines[:len(newLines)-1]
	}

	return int64(len(newLines)), int64(len(oldLines))
}
