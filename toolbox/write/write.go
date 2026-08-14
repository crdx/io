package write

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"crdx.org/io/internal/util"
	"crdx.org/io/tool"
)

// Args is what a write takes.
type Args struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// New builds the write tool confined to root, creating parent directories on the way.
func New(root *os.Root) tool.Tool {
	return tool.Define(
		"write",
		"write a file, creating or overwriting it",
		tool.Schema{
			tool.String("path", "file"),
			tool.String("content", "full contents"),
		},
		Render,
		func(args Args) (string, error) { return exec(root, args) },
	)
}

// Render says how much is being written rather than what.
func Render(args Args) string {
	return fmt.Sprintf("%s (%d bytes)", args.Path, len(args.Content))
}

func exec(root *os.Root, args Args) (string, error) {
	if args.Path == "" {
		return "", errors.New("path is required")
	}

	name, err := util.RootName(root, args.Path)
	if err != nil {
		return "", err
	}

	if util.WithinGitDir(name) {
		return "", util.ErrGitDir
	}

	if directory := filepath.Dir(name); directory != "." {
		if err := root.MkdirAll(directory, 0o755); err != nil {
			return "", err
		}
	}

	if err := root.WriteFile(name, []byte(args.Content), 0o644); err != nil {
		return "", err
	}

	return fmt.Sprintf("wrote %d bytes to %s", len(args.Content), args.Path), nil
}
