package workspaceDir

import (
	"fmt"
	"os"
	"strings"

	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/oh/work"
)

const (
	full  = "full"
	base  = "base"
	short = "short"

	separator = string(os.PathSeparator)
)

type state struct {
	value string
}

func New(workspace *work.Space) segment.Factory {
	return func(options segment.Options) (segment.Segment, error) {
		var args struct {
			Type string `toml:"type"`
		}

		if err := options.Read(&args); err != nil {
			return nil, err
		}

		var value string

		switch args.Type {
		case "", base:
			value = workspace.GetName()
		case short:
			value = workspace.GetShortDir()
		case full:
			value = workspace.GetDir()
		default:
			return nil, fmt.Errorf(
				"type is %q, and wants to be omitted or %q, %q, or %q",
				args.Type,
				base,
				short,
				full,
			)
		}

		return state{value: value}, nil
	}
}

// Render holds the basename forward, since that is the name the user calls the workspace by, and
// leaves whatever leads up to it a step behind.
func (self state) Render(segment.Context) string {
	leading, name := splitLeadingPath(self.value)

	if leading == "" {
		return style.Normal(name)
	}

	return style.Subtle(leading) + style.Normal(name)
}

func splitLeadingPath(path string) (string, string) {
	at := strings.LastIndex(path, separator)
	if at < 0 || at == len(path)-len(separator) {
		return "", path
	}

	return path[:at+len(separator)], path[at+len(separator):]
}
