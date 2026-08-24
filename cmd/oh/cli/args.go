package cli

import (
	"errors"
	"strings"

	"crdx.org/duckopt/v2"
	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/models"
)

const usage = `oh — coding harness

Usage:
    $0 [options] [<prompt>...]

Options:
    -d, --workspace <dir>                  Set working directory and project scope
    -r, --resume <session>                 Resume the saved session by name
    -s, --sessions                         Choose a saved session to resume
    -m, --model <provider/model@effort>    Select the provider, model, and reasoning effort
    -c, --caps <flags>                     Capabilities: rxwgb (read, exec, write, git, bg) [default: rxw]
    -l, --list                             List the available models, then exit
    -u, --update                           Update the cached model list, then exit
    -V, --version                          Show the version
    -h, --help                             Show this help

Model selection takes the closest reading of what you name: the whole name first, then an opening,
then a fragment, so -m sol@hi is enough. An effort of off asks for none, where the model takes it.

Environment:
    OH_ENDPOINT_URL     Talk to somewhere other than the provider's default endpoint
`

// Input is the command line as accepted by duckopt.
type Input struct {
	Message      []string `docopt:"<prompt>"`
	WorkspaceDir string   `docopt:"--workspace"`
	Session      string   `docopt:"--resume"`
	Sessions     bool     `docopt:"--sessions"`
	Model        string   `docopt:"--model"`
	Caps         string   `docopt:"--caps"`
	List         bool     `docopt:"--list"`
	Update       bool     `docopt:"--update"`
	Version      bool     `docopt:"--version"`
}

// Options are the validated options needed to start or resume a conversation.
type Options struct {
	Message      string
	WorkspaceDir string
	Session      string
	Provider     string
	Model        string
	Effort       string
	Caps         caps.Set
}

// Bind reads and validates the process command line.
func Bind() *Input {
	return duckopt.MustBind[Input](usage, "$0")
}

// Resuming reports whether the options name a stored session.
func (self Options) Resuming() bool {
	return self.Session != ""
}

// Parse validates an input command line and resolves its requested model selection.
func (self Input) Parse(modelCachePath string) (Options, error) {
	options := Options{
		WorkspaceDir: self.WorkspaceDir,
		Message:      strings.Join(self.Message, " "),
		Session:      self.Session,
	}

	if self.Model != "" {
		providerName, model, effort, err := models.ParseSelection(modelCachePath, self.Model)
		if err != nil {
			return options, err
		}
		options.Provider = providerName
		options.Model = model
		options.Effort = effort
	}

	grantedCaps, err := caps.Parse(self.Caps)
	if err != nil {
		return options, err
	}
	options.Caps = grantedCaps

	if options.Resuming() && options.WorkspaceDir != "" {
		return options, errors.New("a resumed conversation takes its directory from the session")
	}

	if options.WorkspaceDir == "" {
		options.WorkspaceDir = "."
	}

	return options, nil
}
