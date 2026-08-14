package read

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"crdx.org/io/internal/util"
	"crdx.org/io/tool"
)

// Args is what a read takes. An absent offset or limit is zero, which means the whole file.
type Args struct {
	Path   string `json:"path"`
	Offset int    `json:"offset"`
	Limit  int    `json:"limit"`
}

// New builds the read tool confined to root.
func New(root *os.Root) tool.Tool {
	return tool.Define(
		"read",
		"read a file",
		tool.Schema{
			tool.String("path", "file"),
			tool.Integer("offset", "first line, 1-indexed").Optional(),
			tool.Integer("limit", "max lines").Optional(),
		},
		Render,
		func(args Args) (string, error) { return exec(root, args) },
	)
}

// Render describes a read as path:first-last.
func Render(args Args) string {
	switch {
	case args.Offset > 0 && args.Limit > 0:
		return fmt.Sprintf("%s:%d-%d", args.Path, args.Offset, args.Offset+args.Limit-1)
	case args.Offset > 0:
		return fmt.Sprintf("%s:%d-", args.Path, args.Offset)
	case args.Limit > 0:
		return fmt.Sprintf("%s:1-%d", args.Path, args.Limit)
	default:
		return args.Path
	}
}

func exec(root *os.Root, args Args) (string, error) {
	if args.Path == "" {
		return "", errors.New("path is required")
	}

	name, err := util.RootName(root, args.Path)
	if err != nil {
		return "", err
	}

	data, err := root.ReadFile(name)
	if err != nil {
		return "", err
	}

	if args.Offset <= 0 && args.Limit <= 0 {
		return string(data), nil
	}

	lines := splitLines(data)

	start := max(args.Offset-1, 0)
	if start >= len(lines) {
		return "", fmt.Errorf(
			"offset %d is past the end of the file (%d lines)", args.Offset, len(lines),
		)
	}

	end := len(lines)
	if args.Limit > 0 && start+args.Limit < end {
		end = start + args.Limit
	}

	return strings.Join(lines[start:end], "\n"), nil
}

func splitLines(text []byte) []string {
	if len(text) == 0 {
		return nil
	}

	lines := strings.Split(string(text), "\n")
	if text[len(text)-1] == '\n' {
		lines = lines[:len(lines)-1]
	}

	return lines
}
