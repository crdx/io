package ls

import (
	"context"
	"errors"
	"slices"
	"strings"

	"crdx.org/io/internal/file"

	"crdx.org/io/tool"
)

type Args struct {
	Path string `json:"path"`
}

var ErrWithheld = errors.New("read access unavailable; ctrl+x r grants it")

func New(root *file.Root, isAllowed func() bool) tool.Tool {
	return tool.Implement(
		tool.Definition{
			Name:        "ls",
			Description: "list a directory",
			Schema: tool.Schema{
				tool.String("path", "directory, defaults to working directory").Optional(),
			},
		},
		Describe,
	).
		FocusPath().
		IsEmbarrassinglyParallel().
		ChangesNothing().
		Requires(isAllowed, ErrWithheld).
		Plain(func(_ context.Context, args Args) (string, error) {
			return exec(root, args)
		})
}

func Describe(args Args) (string, string) {
	if args.Path == "." {
		return "", ""
	}

	return args.Path, ""
}

func exec(root *file.Root, args Args) (string, error) {
	root, name, err := root.Resolve(args.Path)
	if err != nil {
		return "", err
	}

	entries, err := root.ReadDir(name)
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
