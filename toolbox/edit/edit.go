package edit

import (
	"errors"
	"os"
	"strings"

	"crdx.org/io/internal/util"
	"crdx.org/io/tool"
)

// Args is what an edit takes.
type Args struct {
	Path    string `json:"path"`
	OldText string `json:"old_text"`
	NewText string `json:"new_text"`
}

// New builds the edit tool confined to root. The text to replace must appear exactly once.
func New(root *os.Root) tool.Tool {
	return tool.Define(
		"edit",
		"replace an exact string in a file, which must appear exactly once",
		tool.Schema{
			tool.String("path", "file"),
			tool.String("old_text", "exact text to replace, including whitespace"),
			tool.String("new_text", "replacement text"),
		},
		Render,
		func(args Args) (string, error) { return exec(root, args) },
	)
}

// Render names the file.
func Render(args Args) string {
	return args.Path
}

func exec(root *os.Root, args Args) (string, error) {
	switch {
	case args.Path == "":
		return "", errors.New("path is required")
	case args.OldText == "":
		return "", errors.New("old_text is required")
	}

	name, err := util.RootName(root, args.Path)
	if err != nil {
		return "", err
	}

	if util.WithinGitDir(name) {
		return "", util.ErrGitDir
	}

	data, err := root.ReadFile(name)
	if err != nil {
		return "", err
	}

	content := string(data)

	switch strings.Count(content, args.OldText) {
	case 0:
		return "", errors.New("old_text does not appear in the file")
	case 1:
	default:
		return "", errors.New(
			"old_text appears more than once, include more context to disambiguate",
		)
	}

	info, err := root.Stat(name)
	if err != nil {
		return "", err
	}

	updated := strings.Replace(content, args.OldText, args.NewText, 1)

	if err := root.WriteFile(name, []byte(updated), info.Mode()); err != nil {
		return "", err
	}

	return "edited " + args.Path, nil
}
