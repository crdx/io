package cli

import (
	"errors"
	"fmt"
	"strings"

	"crdx.org/duckopt/v2"
	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/model"
)

const defaultCapFlags = "rx"

var usage = fmt.Sprintf(`oh — coding harness

Usage:
    $0 [options] [-t <tool>]... [--add <file>]... [<prompt>...]

Options:
    -d, --workspace <dir>                  Set working directory and project scope
    -r, --resume <session>                 Resume the saved session by name
    --from <session>                       Start a new conversation from a saved session
    -s, --sessions                         Choose a saved session to resume
    -m, --model <provider/model@effort>    Select the provider, model, and reasoning effort
    -c, --caps <flags>                     Capabilities: rxwbgs (read, exec, write, bg, git, web) (default: %s)
    -t, --tool <tool>                      Enable a tool; may be repeated (all by default)
    --add <file>                           Add a file to the agent scratch directory; may be repeated
    -l, --list                             List the available models, then exit
    -u, --update                           Update the cached model list, then exit
    -v, --version                          Show version
    -h, --help                             Show this help

Environment:
    OH_ENDPOINT_URL     Talk to somewhere other than the provider's default endpoint
`, defaultCapFlags)

// Input is the command line as accepted by duckopt.
type Input struct {
	Message       []string `docopt:"<prompt>"`
	WorkspaceDir  string   `docopt:"--workspace"`
	Session       string   `docopt:"--resume"`
	SourceSession string   `docopt:"--from"`
	Sessions      bool     `docopt:"--sessions"`
	Model         string   `docopt:"--model"`
	Caps          string   `docopt:"--caps"`
	Tools         []string `docopt:"--tool"`
	AddedFiles    []string `docopt:"--add"`
	List          bool     `docopt:"--list"`
	Update        bool     `docopt:"--update"`
	Version       bool     `docopt:"--version"`
}

// Options are the validated options needed to start or resume a conversation.
type Options struct {
	Message        string
	WorkspaceDir   string
	Session        string
	SourceSession  string
	Provider       string
	Model          string
	Effort         string
	Caps           caps.Set
	WereCapsChosen bool
	Tools          []string
	AddedFiles     []string
}

// Bind reads and validates the process command line.
func Bind() *Input {
	return duckopt.MustBind[Input](usage, "$0")
}

// Resuming reports whether the options name a stored session to continue.
func (self Options) Resuming() bool {
	return self.Session != ""
}

// StartingFromSession reports whether a stored session supplies the new conversation's context.
func (self Options) StartingFromSession() bool {
	return self.SourceSession != ""
}

// Parse validates an input command line and resolves its requested model selection.
func (self Input) Parse(modelCachePath string) (Options, error) {
	options := Options{
		WorkspaceDir:  self.WorkspaceDir,
		Message:       strings.Join(self.Message, " "),
		Session:       self.Session,
		SourceSession: self.SourceSession,
		Tools:         self.Tools,
		AddedFiles:    self.AddedFiles,
	}

	if self.Model != "" {
		providerName, model, effort, err := model.ParseSelection(modelCachePath, self.Model)
		if err != nil {
			return options, err
		}
		options.Provider = providerName
		options.Model = model
		options.Effort = effort
	}

	capFlags := self.Caps
	if capFlags == "" {
		capFlags = defaultCapFlags
	}

	grantedCaps, err := caps.Parse(capFlags)
	if err != nil {
		return options, err
	}
	options.Caps = grantedCaps
	options.WereCapsChosen = self.Caps != ""

	if options.Resuming() && options.StartingFromSession() {
		return options, errors.New("a conversation cannot be resumed while another session supplies its context")
	}
	if options.Resuming() && options.WorkspaceDir != "" {
		return options, errors.New("a resumed conversation takes its directory from the session")
	}
	if options.StartingFromSession() && options.WorkspaceDir != "" {
		return options, errors.New("a conversation started from a session takes its directory from that session")
	}
	if options.Resuming() && len(options.AddedFiles) > 0 {
		return options, errors.New("files cannot be added to a resumed conversation")
	}

	if options.WorkspaceDir == "" {
		options.WorkspaceDir = "."
	}

	return options, nil
}
