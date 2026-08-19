package edit

import (
	"context"
	"errors"
	"strings"

	"crdx.org/io/internal/file"
	"crdx.org/io/internal/pathutil"
	"crdx.org/io/internal/strutil"
	"crdx.org/io/tool"
)

// Args is what an edit takes.
type Args struct {
	Path    string `json:"path"`     // the file to edit
	OldText string `json:"old_text"` // the exact text to replace
	NewText string `json:"new_text"` // the replacement text
}

// New builds an edit tool confined to root.
func New(root *file.Root) tool.Tool {
	return editor{
		Tool: tool.FocusPath(tool.Implement(
			tool.Definition{
				Name:        "edit",
				Description: "replace an exact string in a file, which must appear exactly once",
				Schema: tool.Schema{
					tool.String("path", "file"),
					tool.String("old_text", "exact text to replace, including whitespace"),
					tool.String("new_text", "replacement text"),
				},
			},
			Render,
		).Measured(func(_ context.Context, args Args) (string, tool.Statistics, error) { return exec(root, args) })),

		root: root,
	}
}

type editor struct {
	tool.Tool // the edit tool itself

	root *file.Root // the tree edited
}

func (self editor) ReadOnly() bool { return self.root.RefuseWrite(".") != nil }

// Render names the file.
func Render(args Args) (string, string) {
	return pathutil.Shorten(args.Path), ""
}

func exec(root *file.Root, args Args) (string, tool.Statistics, error) {
	switch {
	case args.Path == "":
		return "", tool.Statistics{}, errors.New("path is required")
	case args.OldText == "":
		return "", tool.Statistics{}, errors.New("old_text is required")
	}

	root, name, err := root.Resolve(args.Path)
	if err != nil {
		return "", tool.Statistics{}, err
	}

	if err := root.RefuseWrite(name); err != nil {
		return "", tool.Statistics{}, err
	}

	data, err := root.ReadFile(name)
	if err != nil {
		return "", tool.Statistics{}, err
	}

	content := string(data)

	switch strings.Count(content, args.OldText) {
	case 0:
		return "", tool.Statistics{}, errors.New("old_text does not appear in the file")
	case 1:
	default:
		return "", tool.Statistics{}, errors.New(
			"old_text appears more than once, include more context to disambiguate",
		)
	}

	info, err := root.Stat(name)
	if err != nil {
		return "", tool.Statistics{}, err
	}

	updatedContent := strings.Replace(content, args.OldText, args.NewText, 1)

	if err := root.WriteFile(name, []byte(updatedContent), info.Mode()); err != nil {
		return "", tool.Statistics{}, err
	}

	addedLines, removedLines := changedLines(args.OldText, args.NewText)
	stats := tool.Statistics{Kind: tool.StatsDiff, Added: addedLines, Removed: removedLines}
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
