package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"crdx.org/duckopt/v2"
	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/cycle"
	"crdx.org/io/cmd/oh/model"
	"crdx.org/io/cmd/oh/startup"
)

const (
	stdinMarker     = "-"
	defaultCapFlags = "rx"
	toolboxSuffix   = ".toml"
)

var usage = `
Usage:
    $0 [options] [-t <tool>]... [-e <env>]... [<prompt>...]
    $0 --login [<provider>]
    $0 --usage [--json]
    $0 --update [--ignored]
    $0 --ctl <command> [<args>...]

Options:
    -r, --resume [<session>]    Resume a session
    -m, --model [<model>]       Choose a model
    -c, --caps <flags>          Set capabilities
    -t, --tool <tool>           Replace the toolbox (name or .toml)
    -e, --env <env>             Replace the toolbox and prompt from a .toml
    -p, --print                 Stream non-interactively
        --ctl                   Run maintenance
        --demo                  Enter the matrix
        --yolo                  Disable the sandbox
    -l, --list                  List available models
    -u, --update                Update model cache
    -L, --login                 Log in to a provider
    -U, --usage                 Show subscription usage
    -I, --ignored               Show ignored items
    -J, --json                  Output as JSON
    -v, --version               Show version
    -h, --help                  Show this help
`

type inputFlags struct {
	IsControlling    bool     `docopt:"--ctl"`
	ControlCommand   string   `docopt:"<command>"`
	ControlArguments []string `docopt:"<args>"`
	Message          []string `docopt:"<prompt>"`
	Login            bool     `docopt:"--login"`
	Provider         string   `docopt:"<provider>"`
	Session          string   `docopt:"--resume"`
	IsSessionPicker  bool     `docopt:"-r"`
	Model            string   `docopt:"--model"`
	IsModelPicker    bool     `docopt:"-m"`
	Caps             string   `docopt:"--caps"`
	Tools            []string `docopt:"--tool"`
	Environments     []string `docopt:"--env"`
	IsPrinting       bool     `docopt:"--print"`
	IsDemoing        bool     `docopt:"--demo"`
	Usage            bool     `docopt:"--usage"`
	JSON             bool     `docopt:"--json"`
	Yolo             bool     `docopt:"--yolo"`
	List             bool     `docopt:"--list"`
	Update           bool     `docopt:"--update"`
	IsShowingIgnored bool     `docopt:"--ignored"`
	Version          bool     `docopt:"--version"`
}

type Input struct {
	inputFlags

	SourceSession string
}

type Options struct {
	Message        string
	Session        string
	SourceSession  string
	Selection      model.Selection
	Caps           caps.Set
	WereCapsChosen bool
	Tools          []string
	Toolboxes      []string
	Environments   []string
	AddedFiles     []startup.InitialFile
	Yolo           bool
	IsPrinting     bool
}

func Bind() *Input {
	originalArgs := os.Args
	arguments := append([]string(nil), os.Args...)
	var sourceSession string
	isSessionPicker := false
	isModelPicker := false
	for i := 1; i < len(arguments); i++ {
		switch {
		case (arguments[i] == "-r" || arguments[i] == "--resume") && (i+1 == len(arguments) || strings.HasPrefix(arguments[i+1], "-")):
			isSessionPicker = true
			arguments = append(arguments[:i], arguments[i+1:]...)
			i--
		case (arguments[i] == "-m" || arguments[i] == "--model") && (i+1 == len(arguments) || strings.HasPrefix(arguments[i+1], "-")):
			isModelPicker = true
			arguments = append(arguments[:i], arguments[i+1:]...)
			i--
		case arguments[i] == "--from" && i+1 < len(arguments):
			sourceSession = arguments[i+1]
			arguments = append(arguments[:i], arguments[i+2:]...)
			i--
		case arguments[i] == "-r" && i+1 < len(arguments) && !strings.HasPrefix(arguments[i+1], "-"):
			arguments[i] = "--resume"
		}
	}

	os.Args = arguments
	defer func() { os.Args = originalArgs }()

	parsedFlags := duckopt.MustBind[inputFlags](usage, "$0")
	parsedFlags.IsSessionPicker = isSessionPicker
	parsedFlags.IsModelPicker = isModelPicker
	parsedFlags.Message = promptAfterStdinMarker(parsedFlags.Message)
	return &Input{inputFlags: *parsedFlags, SourceSession: sourceSession}
}

func partitionToolValues(values []string) ([]string, []string, error) {
	var names, toolboxValues []string

	for _, value := range values {
		if strings.HasSuffix(value, toolboxSuffix) {
			toolboxValues = append(toolboxValues, value)
			continue
		}

		names = append(names, value)
	}

	paths, err := absoluteTomlPaths(toolboxValues)
	if err != nil {
		return nil, nil, err
	}

	return names, paths, nil
}

func absoluteTomlPaths(values []string) ([]string, error) {
	var paths []string

	for _, value := range values {
		if !strings.HasSuffix(value, toolboxSuffix) {
			return nil, fmt.Errorf("%s is not a %s file", value, toolboxSuffix)
		}

		path, err := filepath.Abs(value)
		if err != nil {
			return nil, fmt.Errorf("could not resolve %s: %w", value, err)
		}
		paths = append(paths, path)
	}

	return paths, nil
}

func promptAfterStdinMarker(words []string) []string {
	if len(words) > 0 && words[0] == stdinMarker {
		return words[1:]
	}

	return words
}

func (self Options) Resuming() bool {
	return self.Session != ""
}

func (self Options) StartingFromSession() bool {
	return self.SourceSession != ""
}

func (self Input) Parse(modelCachePath string, defaults model.Defaults) (Options, error) {
	toolNames, toolboxes, err := partitionToolValues(self.Tools)
	if err != nil {
		return Options{}, err
	}

	environments, err := absoluteTomlPaths(self.Environments)
	if err != nil {
		return Options{}, err
	}

	options := Options{
		Message:       strings.Join(self.Message, " "),
		Session:       self.Session,
		SourceSession: self.SourceSession,
		Tools:         toolNames,
		Toolboxes:     toolboxes,
		Environments:  environments,
		Yolo:          self.Yolo,
		IsPrinting:    self.IsPrinting,
	}

	if self.Model != "" {
		selection, err := model.ParseSelection(modelCachePath, self.Model, defaults)
		if err != nil {
			return options, err
		}
		options.Selection = selection
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

	return options, nil
}

func (self Input) Check(isPromptPiped bool) error {
	if self.isResuming() && self.isChoosingModel() {
		return errors.New(
			"a resumed conversation preserves its model; start a new session to choose another",
		)
	}

	if self.isResuming() && (len(self.Tools) > 0 || len(self.Environments) > 0) {
		return errors.New(
			"a resumed conversation preserves its toolbox; start a new session to change them",
		)
	}

	if self.IsDemoing {
		if self.isResuming() || self.SourceSession != "" {
			return errors.New("the simulation keeps nothing, so there is no session of its to resume")
		}

		if self.isChoosingModel() {
			return errors.New("the simulation answers in place of a model, so there is none to choose")
		}
	}

	if !self.IsPrinting {
		return nil
	}

	if self.IsSessionPicker || self.IsModelPicker {
		return errors.New("a printed session cannot open a picker; name the session or the model instead")
	}

	if len(self.Message) == 0 && self.SourceSession == "" && !isPromptPiped {
		return errors.New("a printed session needs a prompt")
	}

	return nil
}

func (self Input) isResuming() bool {
	return self.IsSessionPicker || self.Session != ""
}

func (self Input) isChoosingModel() bool {
	return self.IsModelPicker || self.Model != ""
}

func InheritedOptions(arguments []string, kind cycle.TransitionKind) []string {
	if kind != cycle.NewSession {
		return nil
	}

	if slices.Contains(arguments, "--yolo") {
		return []string{"--yolo"}
	}

	return nil
}
