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
	"crdx.org/io/cmd/oh/skill"
	"crdx.org/io/cmd/oh/store"
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
    -V, --version            Show the version
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

type InputOpts struct {
	Message      []string `docopt:"<prompt>"`
	WorkspaceDir string   `docopt:"--workspace"`
	Session      string   `docopt:"<session>"`
	Caps         string   `docopt:"--caps"`
	Resume       bool     `docopt:"--resume"`
	Version      bool     `docopt:"--version"`
}

type Opts struct {
	workspaceDir   string // the workspace dir
	initialMessage string // the first prompt
	resume         bool   // whether to choose a session
	session        string // the session to resume
	caps           caps   // initial capabilities
}

func (opts InputOpts) parse() (Opts, error) {
	self := Opts{
		workspaceDir:   opts.WorkspaceDir,
		initialMessage: strings.Join(opts.Message, " "),
		resume:         opts.Resume,
		session:        opts.Session,
	}

	grantedCaps, err := Caps(opts.Caps)
	if err != nil {
		return self, err
	}

	self.caps = grantedCaps

	if self.resume && self.workspaceDir != "" {
		return self, errors.New("a resumed conversation takes its directory from the session")
	}

	if self.workspaceDir == "" {
		self.workspaceDir = "."
	}

	return self, nil
}

func run() ([]string, error) {
	inputArgs := duckopt.MustBind[InputOpts](usage, "$0")

	if inputArgs.Version {
		fmt.Println(version())
		return nil, nil
	}

	args, err := inputArgs.parse()
	if err != nil {
		return nil, err
	}

	resumedSession, err := chooseSession(args.resume, args.session)

	switch {
	case errors.Is(err, picker.ErrCancelled):
		return nil, nil
	case err != nil:
		return nil, err
	case resumedSession != nil:
		if resumedSession.Meta.Provider != "" && resumedSession.Meta.Provider != "codex" {
			return nil, fmt.Errorf("cannot resume a %s session with codex", resumedSession.Meta.Provider)
		}
		args.workspaceDir = resumedSession.Meta.WorkspaceDir
	}

	root, err := os.OpenRoot(args.workspaceDir)
	if err != nil {
		return nil, err
	}

	defer func() { _ = root.Close() }()

	workspaceDir, err := filepath.Abs(root.Name())
	if err != nil {
		return nil, fmt.Errorf("could not resolve the workspace path: %w", err)
	}

	homeDir := shellHomeDir()
	if homeDir == "" {
		return nil, errors.New("could not find a home for shell configuration")
	}

	homeDir, err = filepath.Abs(homeDir)
	if err != nil {
		return nil, fmt.Errorf("could not resolve the shell home path: %w", err)
	}

	if err := os.MkdirAll(homeDir, 0o700); err != nil {
		return nil, fmt.Errorf("could not prepare the shell home: %w", err)
	}

	mode := NewMode(args.caps)

	if resumedSession != nil {
		mode = NewResumedMode(args.caps)
	}

	files := file.New(root, refuseWrite(mode))

	settings, err := loadConfiguredSettings(configPath())
	if err != nil {
		return nil, err
	}
	settings.Sandbox, err = keepExistingConfiguredPaths(settings.Sandbox, os.Stderr)
	if err != nil {
		return nil, err
	}
	configuredRoots, err := mountConfiguredPaths(files, mode, settings.Sandbox)
	if err != nil {
		return nil, err
	}
	defer closeConfiguredRoots(configuredRoots)

	globalSkillDirs := append([]string{configDir("skills")}, settings.Skill.Include...)
	availableSkills, err := skill.Discover(workspaceDir, globalSkillDirs, os.Stderr)
	if err != nil {
		return nil, err
	}

	skillRoots, err := skill.MountGlobalSkills(files, availableSkills)
	if err != nil {
		return nil, err
	}
	defer skill.Close(skillRoots)

	processes := sandbox.NewProcesses(args.caps.has(capBackground))
	defer func() { _, _ = processes.Disable() }()

	client := connect(os.Getenv(endpointVariable))

	client.Model = codex.Model
	if settings.Model != "" {
		client.Model = settings.Model
	}
	client.Effort = codex.Effort
	if settings.Effort != "" {
		client.Effort = settings.Effort
	}

	if resumedSession != nil {
		if resumedSession.Meta.Model != "" {
			client.Model = resumedSession.Meta.Model
		}

		if resumedSession.Meta.Effort != "" {
			client.Effort = resumedSession.Meta.Effort
		}
	}

	meta := store.Meta{
		Model:        client.Model,
		WorkspaceDir: workspaceDir,
		Provider:     "codex",
		Effort:       client.Effort,
	}

	log, err := openSession(resumedSession, meta)
	if err != nil {
		return nil, err
	}
	defer func() { _ = log.Close() }()

	tmpDir, err := openTmpDir(log.ID())
	if err != nil {
		return nil, err
	}

	defer func() {
		if !log.Stored() {
			_ = os.Remove(tmpDir)
		}
	}()

	var context string
	var contextFiles []contextFile
	if resumedSession != nil && resumedSession.Meta.Context != "" {
		context = resumedSession.Meta.Context
	} else {
		context, contextFiles, err = loadContext(
			root,
			workspaceDir,
			tmpDir,
			args.caps,
			settings.Sandbox,
			availableSkills,
		)
		if err != nil {
			return nil, err
		}
	}

	if resumedSession == nil {
		meta.Context = context
		if err := log.SetMeta(meta); err != nil {
			return nil, err
		}
	}

	tmpRoot, err := mountTmpDir(files, tmpDir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tmpRoot.Close() }()

	tools := toolbox.Rummage(files)
	shell := confinedShell(workspaceDir, homeDir, tmpDir, settings.Sandbox, mode, files, processes)

	tools = append(tools, shell)
	tools = truncate.Tools(tools)

	chat := &conversation{
		assistant:          agent.New(context, client, tools),
		screen:             output.New(os.Stdout),
		log:                log,
		workspaceDir:       workspaceDir,
		mode:               mode,
		processes:          processes,
		shell:              shell.Name(),
		getOnWithItMessage: settings.GetOnWithItMessage,

		label: func(pending bool, frame int, running bool) string {
			currentCaps := mode.Current()

			return banner(
				client.Model,
				client.Effort,
				workspaceDir,
				tools,
				currentCaps.has(capShell),
				currentCaps.has(capGit),
				currentCaps.has(capBackground),
				pending,
				frame,
				running,
			)
		},
	}

	if resumedSession != nil {
		chat.restore(resumedSession)
	}

	projectSkills, globalSkills := skill.Counts(availableSkills)
	startupElapsed := time.Since(startedAt)
	startup := startupInfo{
		contextFiles:  contextFiles,
		projectSkills: projectSkills,
		globalSkills:  globalSkills,
		toolBytes:     codex.ToolsSize(tools),
	}
	if resumedSession == nil {
		banner := renderStartupBanner(startupElapsed, false, startup)
		chat.notify(theme.Subtle("[") + banner + theme.Subtle("]"))
	}

	chat.makeIntroductions(args.initialMessage)

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

func chooseSession(resume bool, id string) (*store.Session, error) {
	if !resume {
		return nil, nil
	}

	if id != "" {
		return store.Read(sessionsDir(), id)
	}

	sessions, err := store.List(sessionsDir())
	if err != nil {
		return nil, err
	}

	if len(sessions) == 0 {
		return nil, errors.New("there are no stored conversations")
	}

	return picker.Session(sessions, os.Stdin, os.Stdout)
}

func openTmpDir(id string) (string, error) {
	tmp := tmpDir(id)

	if err := os.MkdirAll(tmp, 0o700); err != nil {
		return "", fmt.Errorf("could not prepare the tmp dir: %w", err)
	}

	return tmp, nil
}

func mountTmpDir(files *file.Root, tmpDir string) (*os.Root, error) {
	tmpRoot, err := os.OpenRoot(tmpDir)
	if err != nil {
		return nil, fmt.Errorf("could not open the tmp dir: %w", err)
	}

	files.Mount(sandbox.TmpDir, file.New(tmpRoot, func(string) error { return nil }))
	return tmpRoot, nil
}

func openSession(resumedSession *store.Session, meta store.Meta) (*store.Writer, error) {
	if resumedSession != nil {
		return store.Open(sessionsDir(), resumedSession.ID)
	}

	return store.Create(sessionsDir(), meta)
}
