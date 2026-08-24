package main

import (
	"context"
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
	"crdx.org/io/provider/anthropic"
	"crdx.org/io/provider/chat"
	"crdx.org/io/provider/codex"
	"crdx.org/io/session"
	"crdx.org/io/tool"
	"crdx.org/io/tool/middleware/truncate"
	"crdx.org/io/toolbox"

	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/models"
	"crdx.org/io/cmd/oh/output"
	"crdx.org/io/cmd/oh/shell"
	"crdx.org/io/cmd/oh/skill"
	"crdx.org/io/cmd/oh/store"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/oh/terminal"
)

const (
	endpointVariable = "OH_ENDPOINT_URL"

	codexProvider      = models.CodexProvider
	opencodeGoProvider = models.OpencodeGoProvider
	anthropicProvider  = models.AnthropicProvider
	opencodeGoEndpoint = "https://opencode.ai/zen/go/v1/chat/completions"

	standInToken = "stand-in"
)

var providerNames = []string{codexProvider, opencodeGoProvider, anthropicProvider}

var defaultEfforts = map[string]string{
	codexProvider:     "high",
	anthropicProvider: "high",
}

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

func main() {
	if kind, word, wanted := completionRequest(os.Args[1:]); wanted {
		writeCompletions(os.Stdout, kind, word)
		return
	}

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
	Sessions     bool     `docopt:"--sessions"`
	Model        string   `docopt:"--model"`
	Caps         string   `docopt:"--caps"`
	List         bool     `docopt:"--list"`
	Update       bool     `docopt:"--update"`
	Version      bool     `docopt:"--version"`
}

type Opts struct {
	message      string // the first message
	workspaceDir string
	session      string // the session to resume, empty to start afresh
	provider     string // the provider selected with the model, empty to use the configured or saved provider
	model        string // the explicitly selected model, empty to use the configured or saved model
	effort       string // the effort paired with an explicitly selected model
	caps         caps.Set
}

func (self Opts) resuming() bool { return self.session != "" }

func (opts InputOpts) parse() (Opts, error) {
	self := Opts{
		workspaceDir: opts.WorkspaceDir,
		message:      strings.Join(opts.Message, " "),
		session:      opts.Session,
	}

	if opts.Model != "" {
		provider, model, effort, err := models.ParseSelection(modelCachePath(), opts.Model)
		if err != nil {
			return self, err
		}
		self.provider = provider
		self.model = model
		self.effort = effort
	}

	grantedCaps, err := caps.Parse(opts.Caps)
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

	if inputArgs.List {
		return nil, models.List(os.Stdout, modelCachePath())
	}

	if inputArgs.Update {
		return nil, models.Update(os.Stdout, os.Getenv(endpointVariable), modelCachePath(), listProviderModels)
	}

	if inputArgs.Sessions {
		sessionName, err := chooseStoredSession(sessionsDir(), os.Stdin, os.Stdout)
		if err != nil {
			return nil, err
		}
		if sessionName == "" {
			return nil, nil
		}

		return []string{"-r", sessionName}, nil
	}

	args, err := inputArgs.parse()
	if err != nil {
		return nil, err
	}

	if err := refuseOutdatedSessions(sessionsDir()); err != nil {
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

	mode := caps.NewMode(args.caps)

	if resumedSession != nil {
		mode = caps.NewResumedMode(args.caps)
	}

	files := file.New(root, caps.RefuseWrite(mode))

	config, err := loadConfig(configPath())
	if err != nil {
		return nil, err
	}
	config.Sandbox, err = shell.PreparePaths(config.Sandbox, os.Stderr)
	if err != nil {
		return nil, err
	}
	configuredRoots, err := shell.MountPaths(files, mode, config.Sandbox)
	if err != nil {
		return nil, err
	}
	defer shell.CloseRoots(configuredRoots)

	globalSkillDirs := append([]string{configDir("skills")}, config.Skill.Include...)
	availableSkills, err := skill.Discover(workspaceDir, globalSkillDirs, os.Stderr)
	if err != nil {
		return nil, err
	}

	availableSkills = skill.ExcludeGlobal(availableSkills, config.Skill.Exclude)

	skillRoots, err := skill.MountGlobalSkills(files, availableSkills)
	if err != nil {
		return nil, err
	}
	defer skill.Close(skillRoots)

	processes := sandbox.NewProcesses(args.caps.Has(caps.Background))
	defer func() { _, _ = processes.Disable() }()

	providerName, model, effort, err := resolveProviderChoice(args.provider, args.model, args.effort, config, resumedSession)
	if err != nil {
		return nil, err
	}
	choice, err := models.Chosen(modelCachePath(), providerName, model)
	if err != nil {
		return nil, err
	}

	client, err := connect(choice, effort, os.Getenv(endpointVariable))
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

	tmpDir, err := openTmpDir(log.Name())
	if err != nil {
		return nil, err
	}

	defer func() {
		if !log.Stored() {
			_ = os.Remove(tmpDir)
		}
	}()

	var systemPrompt string
	var contextFiles []contextFile
	if resumedSession != nil && resumedSession.Meta.SystemPrompt != "" {
		systemPrompt = resumedSession.Meta.SystemPrompt
	} else {
		systemPrompt, contextFiles, err = loadContext(
			root,
			workspaceDir,
			log.Name(),
			tmpDir,
			homeDir,
			args.caps,
			config.Sandbox,
			availableSkills,
		)
		if err != nil {
			return nil, err
		}
	}

	if resumedSession == nil {
		meta.SystemPrompt = systemPrompt
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
	commandShell := shell.New(workspaceDir, homeDir, tmpDir, config.Sandbox, mode, files, processes)

	tools = append(tools, commandShell)
	tools = truncate.Tools(tools)

	chat := &Harness{
		agent:    agent.New(systemPrompt, client, tools),
		screen:   output.New(os.Stdout).LinkPathsUnder(workspaceDir),
		terminal: terminal.New(os.Stdout),

		log: log,

		workspaceDir:        workspaceDir,
		contextWindowTokens: choice.ContextWindowTokens,
		mode:                mode,
		processes:           processes,
		onTurnFinished:      func() { sendTurnFinishedNotification(workspaceDir) },
		getOnWithItMessage:  config.GetOnWithItMessage,
	}

	chat.segmentLayout, err = config.layout(availableSegments(workspaceDir, log.Name(), model, effort, chat))
	if err != nil {
		return nil, err
	}

	if err := config.unknownKeys(); err != nil {
		return nil, err
	}

	if resumedSession != nil {
		chat.restore(resumedSession)
	}

	projectSkills, globalSkills := skill.Counts(availableSkills)
	startupElapsed := time.Since(startedAt)
	startup := startupInfo{
		Session:       log.Name(),
		ContextFiles:  startupFilesOf(contextFiles),
		ProjectSkills: projectSkills,
		GlobalSkills:  globalSkills,
		ToolBytes:     client.toolsSize(tools),
	}
	if resumedSession == nil {
		chat.notify(startupEvent(startupElapsed, startup))
	}

	chat.begin(args.message)

	if chat.restart != nil {
		return chat.restart, nil
	}

	if log.Stored() {
		fmt.Println(style.Subtle(resumeParams(log.Name())))
	}

	return nil, nil
}

func resumeParams(reference string) string {
	return fmt.Sprintf("%s -r %s", filepath.Base(os.Args[0]), reference)
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

func resolveProviderChoice(
	requestedProvider string,
	requestedModel string,
	requestedEffort string,
	config Config,
	resumedSession *store.Session,
) (string, string, string, error) {
	providerName := config.Provider
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
	model := config.Model
	effort := config.Effort
	if effort == "" {
		effort = defaultEfforts[providerName]
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

	return providerName, model, models.ResolveEffort(effort), nil
}

func connect(choice models.Choice, effort string, endpoint string) (*connection, error) {
	switch choice.Provider {
	case codexProvider:
		tokens := codex.StoredCredentials()
		address := codex.Endpoint

		if endpoint != "" {
			tokens = codex.Static(standInToken, standInToken)
			address = endpoint
		}

		client, err := codex.New(tokens, choice.Model, effort)
		if err != nil {
			return nil, err
		}
		client.URL = address

		return &connection{providerClient: client, toolsSize: codex.ToolsSize}, nil

	case opencodeGoProvider:
		token := standInToken
		if endpoint == "" {
			endpoint = opencodeGoEndpoint

			var err error
			if token, err = chat.StoredKey(); err != nil {
				return nil, err
			}
		}
		client, err := chat.New(endpoint, token, choice.Model, effort, choice.MaxOutputTokens)
		if err != nil {
			return nil, err
		}

		return &connection{providerClient: client, toolsSize: chat.ToolsSize}, nil

	case anthropicProvider:
		tokens := anthropic.StoredCredentials()
		address := anthropic.Endpoint

		if endpoint != "" {
			tokens = anthropic.Static(standInToken)
			address = endpoint
		}

		client, err := anthropic.New(tokens, choice.Model, effort, choice.MaxOutputTokens)
		if err != nil {
			return nil, err
		}
		client.URL = address

		return &connection{providerClient: client, toolsSize: anthropic.ToolsSize}, nil

	default:
		return nil, fmt.Errorf("unknown provider %q", choice.Provider)
	}
}

func listProviderModels(ctx context.Context, providerName string, endpoint string) ([]agent.Model, error) {
	client, err := connect(models.Choice{Provider: providerName}, "", endpoint)
	if err != nil {
		return nil, err
	}

	lister, canList := client.providerClient.(agent.Lister)
	if !canList {
		return nil, nil
	}

	return lister.Models(ctx)
}

func loadSession(name string) (*store.Session, error) {
	if name == "" {
		return nil, nil
	}

	return store.Read(sessionsDir(), name)
}

func openTmpDir(name string) (string, error) {
	tmp := tmpDir(name)

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
	if resumedSession == nil {
		return store.Create(sessionsDir(), meta)
	}

	log, err := store.Open(sessionsDir(), resumedSession.Name)
	if errors.Is(err, session.ErrInUse) {
		return nil, fmt.Errorf("session %s is already open", resumedSession.Name)
	}

	return log, err
}
