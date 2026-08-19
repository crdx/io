package ls

import (
	"context"
	"slices"
	"strings"

	"crdx.org/io/internal/file"
	"crdx.org/io/internal/pathutil"
	"crdx.org/io/tool"
)

// Args is what a listing takes. An absent path is the working directory.
type Args struct {
	Path string `json:"path"` // the directory to list
}

// New builds the ls tool confined to root. A directory is marked with a trailing slash.
func New(root *file.Root) tool.Tool {
	return tool.ReadOnly(tool.Concurrent(tool.FocusPath(tool.Implement(
		tool.Definition{
			Name:        "ls",
			Description: "list a directory",
			Schema: tool.Schema{
				tool.String("path", "directory, defaults to working directory").Optional(),
			},
		},
		Render,
	).Plain(func(_ context.Context, args Args) (string, error) {
		return exec(root, args)
	}))))
}

// Render names the path, the working directory going without saying.
func Render(args Args) (string, string) {
	if args.Path == "." {
		return "", ""
	}

	return pathutil.Shorten(args.Path), ""
}

func exec(root *file.Root, args Args) (string, error) {
	root, name, err := root.Resolve(args.Path)
	if err != nil {
		return "", err
	}

	directory, err := root.Open(name)
	if err != nil {
		return "", err
	}

	defer func() { _ = directory.Close() }()

	entries, err := directory.ReadDir(-1)
	if err != nil {
		return "", err
	}

	names := make([]string, 0, len(entries))

	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name()+"/")
		} else {
			names = append(names, entry.Name())
		}
	}

	if len(names) == 0 {
		return "(empty)", nil
	}

	slices.Sort(names)

	return strings.Join(names, "\n"), nil
}
