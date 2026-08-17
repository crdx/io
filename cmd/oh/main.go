package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"crdx.org/duckopt/v2"
	"crdx.org/io/agent"
	"crdx.org/io/internal/file"
	"crdx.org/io/internal/sandbox"
	"crdx.org/io/provider/codex"
	"crdx.org/io/tool/middleware/truncate"
	"crdx.org/io/toolbox"

	"crdx.org/io/cmd/oh/output"
	"crdx.org/io/cmd/oh/picker"
	"crdx.org/io/cmd/oh/session"
	"crdx.org/io/cmd/oh/theme"
)

const endpointVariable = "OH_ENDPOINT_URL"

const usage = `oh — coding harness

Usage:
    $0 [options] --resume [<session>]
    $0 [options] [<prompt>...]

Options:
    -d, --workspace <dir>    Set working directory and project scope
    -r, --resume             Resume a saved session
    -c, --caps <letters>     Capabilities: rwxgb (read, write, exec, git, bg) [default: rwx]
    -h, --help               Show this help

Environment:
    OH_ENDPOINT_URL     Talk to somewhere other than the real endpoint
`

func main() {
	sandbox.Init()

	theme.Init(os.Stdout)

	args, err := run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if args != nil {
		self, err := os.Executable()
		if err == nil {
			//nolint:gosec // the binary is this one, and the arguments are ours
			err = syscall.Exec(self, append([]string{self}, args...), os.Environ())
		}

		fmt.Fprintln(os.Stderr, "could not restart:", err)
		os.Exit(1)
	}
}

type Opts struct {
	Message   []string `docopt:"<prompt>"`
	Workspace string   `docopt:"--workspace"`
	Session   string   `docopt:"<session>"`
	Caps      string   `docopt:"--caps"`
	Resume    bool     `docopt:"--resume"`
}

type invocation struct { // what the arguments amount to once settled against each other
	workspace      string // the workspace
	initialMessage string // the first prompt
	resume         bool   // whether to choose a session
	session        string // the session to resume
	caps           caps   // initial capabilities
}

func (opts Opts) invocation() (invocation, error) {
	self := invocation{
		workspace:      opts.Workspace,
		initialMessage: strings.Join(opts.Message, " "),
		resume:         opts.Resume,
		session:        opts.Session,
	}

	grantedCaps, err := Caps(opts.Caps)
	if err != nil {
		return self, err
	}

	self.caps = grantedCaps

	if self.resume && self.workspace != "" {
		return self, errors.New("a resumed conversation takes its directory from the session")
	}

	if self.workspace == "" {
		self.workspace = "."
	}

	return self, nil
}

func run() ([]string, error) {
	invocation, err := duckopt.MustBind[Opts](usage, "$0").invocation()
	if err != nil {
		return nil, err
	}

	resumedSession, err := chooseSession(invocation.resume, invocation.session)

	switch {
	case errors.Is(err, picker.ErrCancelled):
		return nil, nil
	case err != nil:
		return nil, err
	case resumedSession != nil:
		if resumedSession.Head.Provider != "" && resumedSession.Head.Provider != "codex" {
			return nil, fmt.Errorf("cannot resume a %s session with codex", resumedSession.Head.Provider)
		}
		invocation.workspace = resumedSession.Head.Workspace
	}

	root, err := os.OpenRoot(invocation.workspace)
	if err != nil {
		return nil, err
	}

	defer func() { _ = root.Close() }()

	workspace, err := filepath.Abs(root.Name())
	if err != nil {
		return nil, fmt.Errorf("could not resolve the workspace path: %w", err)
	}

	home := shellHome()
	if home == "" {
		return nil, errors.New("could not find a home for shell configuration")
	}

	home, err = filepath.Abs(home)
	if err != nil {
		return nil, fmt.Errorf("could not resolve the shell home path: %w", err)
	}

	if err := os.MkdirAll(home, 0o700); err != nil {
		return nil, fmt.Errorf("could not prepare the shell home: %w", err)
	}

	grantedCaps := invocation.caps

	mode := NewMode(grantedCaps)

	if resumedSession != nil {
		mode = NewResumedMode(grantedCaps)
	}

	files := file.New(root, refusal(mode))
	processes := sandbox.NewProcesses(grantedCaps.has(capBackground))
	defer func() { _, _ = processes.Disable() }()

	client := connect(os.Getenv(endpointVariable))

	client.Model = codex.Model
	client.Effort = codex.Effort

	if resumedSession != nil {
		if resumedSession.Head.Model != "" {
			client.Model = resumedSession.Head.Model
		}

		if resumedSession.Head.Effort != "" {
			client.Effort = resumedSession.Head.Effort
		}
	}

	system := prompt(workspace, grantedCaps)

	if resumedSession != nil && resumedSession.Head.Prompt != "" {
		system = resumedSession.Head.Prompt
	}

	log, err := openSession(resumedSession, session.Header{
		Model:     client.Model,
		Workspace: workspace,
		Provider:  "codex",
		Effort:    client.Effort,
		Prompt:    system,
	})
	if err != nil {
		return nil, err
	}

	defer func() { _ = log.Close() }()

	tools := toolbox.Rummage(files)

	tmp, err := openTmpDir(log.ID())
	if err != nil {
		return nil, err
	}

	defer func() {
		if !log.Stored() {
			_ = os.Remove(tmp) // takes nothing that is not empty regardless
		}
	}()

	tmpRoot, err := mountTmpDir(files, tmp)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tmpRoot.Close() }()

	tools = append(tools, confinedShell(workspace, home, tmp, mode, files, processes))
	tools = truncate.Tools(tools)

	chat := &conversation{
		assistant: agent.New(system, client, tools),
		screen:    output.New(os.Stdout),
		log:       log,
		workspace: workspace,
		mode:      mode,
		processes: processes,

		label: func(pending bool, frame int, running bool) string {
			currentCaps := mode.Current()
			return banner(client.Model, client.Effort, workspace, tools, currentCaps.has(capShell),
				currentCaps.has(capGit), currentCaps.has(capBackground), pending, frame, running)
		},
	}

	if resumedSession != nil {
		chat.restore(resumedSession)
	}

	startupBanner := renderStartupBanner(time.Since(startedAt), resumedSession != nil)
	if startupBanner != "" {
		chat.notify("[" + theme.Subtle(startupBanner) + "]")
	}

	chat.makeIntroductions(invocation.initialMessage)

	if chat.restart != nil {
		return chat.restart, nil
	}

	if log.Stored() {
		fmt.Println(theme.Subtle(resumeParams(log.ID())))
	}

	return nil, nil
}

func resumeParams(id string) string {
	return fmt.Sprintf("%s --resume %s", filepath.Base(os.Args[0]), id)
}

func connect(endpoint string) *codex.Client {
	if endpoint == "" {
		return codex.Auth()
	}

	client := codex.New(codex.Static("fake", "fake"))
	client.URL = endpoint

	return client
}

func chooseSession(resume bool, id string) (*session.Session, error) {
	if !resume {
		return nil, nil
	}

	if id != "" {
		return session.Read(sessionsDir(), id) // named rather than chosen
	}

	sessions, err := session.List(sessionsDir())
	if err != nil {
		return nil, err
	}

	if len(sessions) == 0 {
		return nil, errors.New("there are no stored conversations")
	}

	return picker.Session(sessions, os.Stdin, os.Stdout)
}

func openTmpDir(id string) (string, error) { // kept, so a resumed conversation finds its files
	tmp := tmpDir(id)

	if err := os.MkdirAll(tmp, 0o700); err != nil {
		return "", fmt.Errorf("could not prepare the tmp dir: %w", err)
	}

	return tmp, nil
}

func mountTmpDir(files *file.Root, tmp string) (*os.Root, error) {
	tmpRoot, err := os.OpenRoot(tmp)
	if err != nil {
		return nil, fmt.Errorf("could not open the tmp dir: %w", err)
	}

	files.Mount(sandbox.TmpDir, file.New(tmpRoot, func(string) error { return nil }))
	return tmpRoot, nil
}

func openSession(resumedSession *session.Session, head session.Header) (*session.Writer, error) {
	if resumedSession != nil {
		return session.Open(sessionsDir(), resumedSession.ID)
	}

	return session.Create(sessionsDir(), head)
}
