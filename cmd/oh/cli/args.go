package cli

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"crdx.org/duckopt/v2"
	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/cycle"
	"crdx.org/io/cmd/oh/model"
)

const defaultCapFlags = "rx"

var usage = fmt.Sprintf(`oh — coding harness

Usage:
    $0 -L [<login-provider>]
    $0 -U [-J]
    $0 [options] [-t <tool>]... [<prompt>...]

Options:
    -L, --login                 Log in to a provider, choosing when omitted
    -r, --resume [<session>]    Open the session picker, or resume a session by name
    -m, --model [<model>]       Open the model picker, or set the provider, model, effort, and optional +fast mode
    -c, --caps <flags>          Set capability flags: rxw gs (read, exec, write, git, web) (default: %s)
    -t, --tool <tool>           Set exclusive tool selection; may be repeated
    -p, --print                 Answer the prompt without an interface, then exit
        --yolo                  Live dangerously and don't sandbox anything
    -U, --usage                 Show subscription usage, then exit
    -J, --json                  Write the usage as JSON rather than drawing it
    -l, --list                  List the available models, then exit
    -u, --update                Update the cached model list, then exit
    -v, --version               Show version
    -h, --help                  Show this help
`, defaultCapFlags)

type inputFlags struct {
	Message         []string `docopt:"<prompt>"`
	Login           bool     `docopt:"--login"`
	LoginProvider   string   `docopt:"<login-provider>"`
	Session         string   `docopt:"--resume"`
	IsSessionPicker bool     `docopt:"-r"`
	Model           string   `docopt:"--model"`
	IsModelPicker   bool     `docopt:"-m"`
	Caps            string   `docopt:"--caps"`
	Tools           []string `docopt:"--tool"`
	IsPrinting      bool     `docopt:"--print"`
	Usage           bool     `docopt:"--usage"`
	JSON            bool     `docopt:"--json"`
	Yolo            bool     `docopt:"--yolo"`
	List            bool     `docopt:"--list"`
	Update          bool     `docopt:"--update"`
	Version         bool     `docopt:"--version"`
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
	AddedFiles     []string
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
	return &Input{inputFlags: *parsedFlags, SourceSession: sourceSession}
}

func (self Options) Resuming() bool {
	return self.Session != ""
}

func (self Options) StartingFromSession() bool {
	return self.SourceSession != ""
}

func (self Input) Parse(modelCachePath string) (Options, error) {
	options := Options{
		Message:       strings.Join(self.Message, " "),
		Session:       self.Session,
		SourceSession: self.SourceSession,
		Tools:         self.Tools,
		Yolo:          self.Yolo,
		IsPrinting:    self.IsPrinting,
	}

	if self.Model != "" {
		selection, err := model.ParseSelection(modelCachePath, self.Model)
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

func InheritedOptions(arguments []string, kind cycle.TransitionKind) []string {
	if kind != cycle.NewSession {
		return nil
	}

	if slices.Contains(arguments, "--yolo") {
		return []string{"--yolo"}
	}

	return nil
}
