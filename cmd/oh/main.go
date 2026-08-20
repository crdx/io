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
	"crdx.org/io/internal/req"
	"crdx.org/io/internal/sandbox"
	"crdx.org/io/provider/chat"
	"crdx.org/io/provider/codex"
	"crdx.org/io/tool"
	"crdx.org/io/tool/middleware/truncate"
	"crdx.org/io/toolbox"

	"crdx.org/io/cmd/oh/output"
	"crdx.org/io/cmd/oh/skill"
	"crdx.org/io/cmd/oh/store"
	"crdx.org/io/cmd/oh/style"
)

const (
	endpointVariable = "OH_ENDPOINT_URL"

	codexProvider      = "codex"
	opencodeGoProvider = "opencode-go"
	opencodeGoEndpoint = "https://opencode.ai/zen/go/v1/chat/completions"
)

const usage = `oh — coding harness

Usage:
    $0 [options] [<prompt>...]

Options:
    -d, --workspace <dir>                  Set working directory and project scope
    -r, --resume <session>                 Resume the saved session
    -m, --model <provider/model@effort>    Select the provider, model, and reasoning effort
    -c, --caps <flags>                     Capabilities: rxwgb (read, exec, write, git, bg) [default: rxw]
    -V, --version                          Show the version
    -h, --help                             Show this help

Environment:
    OH_ENDPOINT_URL     Talk to somewhere other than the provider's default endpoint
`

func main() {
	sandbox.Init()

	style.Init(os.Stdout)

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
	Session      string   `docopt:"--resume"`
	Model        string   `docopt:"--model"`
	Caps         string   `docopt:"--caps"`
	Version      bool     `docopt:"--version"`
}

type Opts struct {
	workspaceDir   string
	initialMessage string // the first prompt
	session        string // the session to resume, empty to start afresh
	provider       string // the provider selected with the model, empty to use the configured or saved provider
	model          string // the explicitly selected model, empty to use the configured or saved model
	effort         string // the effort paired with an explicitly selected model
	caps           caps
}

func (self Opts) resuming() bool { return self.session != "" }

func (opts InputOpts) parse() (Opts, error) {
	self := Opts{
		workspaceDir:   opts.WorkspaceDir,
		initialMessage: strings.Join(opts.Message, " "),
		session:        opts.Session,
	}

	if opts.Model != "" {
		provider, model, effort, err := parseModelSelection(opts.Model)
		if err != nil {
			return self, err
		}
		self.provider = provider
		self.model = model
		self.effort = effort
	}

	grantedCaps, err := Caps(opts.Caps)
	if err != nil {
		return self, err
	}

	self.caps = grantedCaps

	if self.resuming() && self.workspaceDir != "" {
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

	resumedSession, err := loadSession(args.session)
	if err != nil {
		return nil, err
	}
	if resumedSession != nil {
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
	if err := ensureWorkspaceIsNotShadowed(workspaceDir); err != nil {
		return nil, err
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

	availableSkills = skill.ExcludeGlobal(availableSkills, settings.Skill.Exclude)

	skillRoots, err := skill.MountGlobalSkills(files, availableSkills)
	if err != nil {
		return nil, err
	}
	defer skill.Close(skillRoots)

	processes := sandbox.NewProcesses(args.caps.has(capBackground))
	defer func() { _, _ = processes.Disable() }()

	providerName, model, effort, err := resolveProviderSettings(args.provider, args.model, args.effort, settings, resumedSession)
	if err != nil {
		return nil, err
	}
	client, err := connect(providerName, model, effort, os.Getenv(endpointVariable))
	if err != nil {
		return nil, err
	}

	meta := store.Meta{
		Model:        model,
		WorkspaceDir: workspaceDir,
		Provider:     providerName,
		Effort:       effort,
	}

	log, err := openSession(resumedSession, meta)
	if err != nil {
		return nil, err
	}
	defer func() { _ = log.Close() }()
	client.ObserveHTTP(log.Observer())

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
			log.ID(),
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

	snapshots := file.NewSnapshots()
	tools := toolbox.Rummage(files, snapshots)
	shell := confinedShell(workspaceDir, homeDir, tmpDir, settings.Sandbox, mode, files, processes)

	tools = append(tools, shell)
	tools = truncate.Tools(tools)

	chat := &conversation{
		assistant:          agent.New(context, client, tools),
		screen:             output.New(os.Stdout).PostProcess(workspaceDir),
		log:                log,
		workspaceDir:       workspaceDir,
		mode:               mode,
		processes:          processes,
		shell:              shell.Name(),
		notifyTurnFinished: func() { sendTurnFinishedNotification(workspaceDir) },
		getOnWithItMessage: settings.GetOnWithItMessage,

		label: func(pending bool, frame int, running bool) string {
			currentCaps := mode.Current()

			return banner(
				model,
				effort,
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
		sessionID:     log.ID(),
		contextFiles:  contextFiles,
		projectSkills: projectSkills,
		globalSkills:  globalSkills,
		toolBytes:     client.toolsSize(tools),
	}
	if resumedSession == nil {
		banner := renderStartupBanner(startupElapsed, false, startup)
		chat.notify(style.Subtle("[") + banner + style.Subtle("]"))
	}

	chat.makeIntroductions(args.initialMessage)

	if chat.restart != nil {
		return chat.restart, nil
	}

	if log.Stored() {
		fmt.Println(style.Subtle(resumeParams(log.ID())))
	}

	return nil, nil
}

func resumeParams(id string) string {
	return fmt.Sprintf("%s -r %s", filepath.Base(os.Args[0]), id)
}

type providerClient interface {
	agent.Provider
	agent.State
	ObserveHTTP(req.Observer)
}

type connection struct {
	providerClient

	toolsSize func([]tool.Tool) int
}

func resolveProviderSettings(
	requestedProvider string,
	requestedModel string,
	requestedEffort string,
	settings configuredSettings,
	resumedSession *store.Session,
) (string, string, string, error) {
	providerName := settings.Provider
	if providerName == "" {
		providerName = codexProvider
	}

	sessionProvider := ""
	if resumedSession != nil {
		sessionProvider = resumedSession.Meta.Provider
	}
	if sessionProvider != "" {
		providerName = sessionProvider
	}
	if requestedProvider != "" {
		providerName = requestedProvider
	}

	if sessionProvider != "" && providerName != sessionProvider {
		return "", "", "", fmt.Errorf("cannot resume a %s session with %s", sessionProvider, providerName)
	}
	model := settings.Model
	effort := settings.Effort
	if effort == "" && providerName == codexProvider {
		effort = codex.Effort
	}
	if resumedSession != nil {
		if resumedSession.Meta.Model != "" {
			model = resumedSession.Meta.Model
		}
		if resumedSession.Meta.Effort != "" {
			effort = resumedSession.Meta.Effort
		}
	}
	if requestedModel != "" {
		model = requestedModel
		effort = requestedEffort
	}
	if model == "" {
		return "", "", "", errors.New("no model selected: use -m provider/model@effort or configure model")
	}

	return providerName, model, effort, nil
}

func connect(providerName string, model string, effort string, endpoint string) (*connection, error) {
	switch providerName {
	case codexProvider:
		var client *codex.Client
		if endpoint == "" {
			client = codex.Auth()
		} else {
			client = codex.New(codex.Static("fake", "fake"))
			client.URL = endpoint
		}
		client.Model = model
		client.Effort = effort
		return &connection{providerClient: client, toolsSize: codex.ToolsSize}, nil

	case opencodeGoProvider:
		token, err := chat.StoredKey()
		if err != nil {
			return nil, err
		}
		if endpoint == "" {
			endpoint = opencodeGoEndpoint
		}
		client := chat.New(endpoint, model, effort, token)
		return &connection{providerClient: client, toolsSize: chat.ToolsSize}, nil

	default:
		return nil, fmt.Errorf("unknown provider %q", providerName)
	}
}

func loadSession(id string) (*store.Session, error) {
	if id == "" {
		return nil, nil
	}

	return store.Read(sessionsDir(), id)
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
