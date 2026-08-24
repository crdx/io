package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"crdx.org/io/agent"
	"crdx.org/io/internal/file"
	"crdx.org/io/internal/req"
	"crdx.org/io/internal/sandbox"
	"crdx.org/io/internal/util/pathutil"
	"crdx.org/io/provider/anthropic"
	"crdx.org/io/provider/chat"
	"crdx.org/io/provider/codex"
	"crdx.org/io/session"
	"crdx.org/io/tool"
	"crdx.org/io/tool/middleware/truncate"
	"crdx.org/io/toolbox"
	"crdx.org/io/toolbox/notify"

	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/cli"
	"crdx.org/io/cmd/oh/commands"
	"crdx.org/io/cmd/oh/config"
	"crdx.org/io/cmd/oh/metrics"
	"crdx.org/io/cmd/oh/model"
	"crdx.org/io/cmd/oh/output"
	"crdx.org/io/cmd/oh/prompt"
	"crdx.org/io/cmd/oh/shell"
	"crdx.org/io/cmd/oh/skill"
	"crdx.org/io/cmd/oh/slash"
	"crdx.org/io/cmd/oh/snippets"
	"crdx.org/io/cmd/oh/startup"
	"crdx.org/io/cmd/oh/store"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/oh/terminal"
)

const (
	endpointVariable = "OH_ENDPOINT_URL"

	codexProvider      = model.CodexProvider
	opencodeGoProvider = model.OpencodeGoProvider
	anthropicProvider  = model.AnthropicProvider
	opencodeGoEndpoint = "https://opencode.ai/zen/go/v1/chat/completions"

	standInToken = "stand-in"
)

var providerNames = []string{codexProvider, opencodeGoProvider, anthropicProvider}

var completableToolNames = []string{"read", "ls", "find", "grep", "write", "edit", "bash", "notify"}

func main() {
	if cli.WriteCompletions(os.Stdout, os.Args[1:], cli.Sources{
		ModelCachePath: modelCachePath(),
		SessionsDir:    sessionsDir(),
		ToolNames:      completableToolNames,
	}) {
		return
	}

	sandbox.Init()

	style.Init(os.Stdout)

	chosenSession, err := run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if chosenSession != "" {
		self, err := os.Executable()
		if err == nil {
			//nolint:gosec // the binary is this one, and the arguments are ours
			err = syscall.Exec(self, []string{self, "-r", chosenSession}, os.Environ())
		}

		fmt.Fprintln(os.Stderr, "could not open the session:", err)
		os.Exit(1)
	}
}

func openingCaps(args cli.Options, resumedSession *store.Session) (caps.Set, error) {
	if resumedSession == nil {
		return args.Caps, nil
	}

	lastCaps, found := caps.LastRecordedMode(resumedSession.Events)
	if !found {
		return args.Caps, nil
	}

	if args.WereCapsChosen && args.Caps != lastCaps {
		return 0, fmt.Errorf(
			"a resumed conversation opens in the mode it was left in, which was %s rather than %s",
			lastCaps.Flags(),
			args.Caps.Flags(),
		)
	}

	return lastCaps, nil
}

func run() (string, error) {
	inputArgs := cli.Bind()

	if inputArgs.Version {
		fmt.Println(version())
		return "", nil
	}

	if inputArgs.List {
		return "", model.List(os.Stdout, modelCachePath())
	}

	if inputArgs.Update {
		return "", model.Update(os.Stdout, os.Getenv(endpointVariable), modelCachePath(), listProviderModels)
	}

	if inputArgs.Sessions {
		return chooseStoredSession(sessionsDir(), os.Stdin, os.Stdout)
	}

	args, err := inputArgs.Parse(modelCachePath())
	if err != nil {
		return "", err
	}

	if err := refuseUnreadableSessions(sessionsDir()); err != nil {
		return "", err
	}

	resumedSession, err := loadSession(args.Session)
	if err != nil {
		return "", err
	}
	if resumedSession != nil {
		args.WorkspaceDir = resumedSession.Meta.WorkspaceDir
	}

	args.Caps, err = openingCaps(args, resumedSession)
	if err != nil {
		return "", err
	}

	root, err := os.OpenRoot(args.WorkspaceDir)
	if err != nil {
		return "", err
	}

	defer func() { _ = root.Close() }()

	workspaceDir, err := filepath.Abs(root.Name())
	if err != nil {
		return "", fmt.Errorf("could not resolve the workspace path: %w", err)
	}
	if err := ensureWorkspaceIsNotShadowed(workspaceDir); err != nil {
		return "", err
	}

	homeDir := shellHomeDir()
	if homeDir == "" {
		return "", errors.New("could not find a home for shell configuration")
	}

	homeDir, err = filepath.Abs(homeDir)
	if err != nil {
		return "", fmt.Errorf("could not resolve the shell home path: %w", err)
	}

	if err := os.MkdirAll(homeDir, 0o700); err != nil {
		return "", fmt.Errorf("could not prepare the shell home: %w", err)
	}

	mode := caps.NewMode(args.Caps)

	if resumedSession != nil {
		mode = caps.NewResumedMode(args.Caps)
	}

	files := file.New(root, caps.RefuseWrite(mode))
	homeRoot, err := mountHomeDir(files, homeDir, mode)
	if err != nil {
		return "", err
	}
	defer func() { _ = homeRoot.Close() }()

	configPath := configFile()
	settings, err := config.Load(configPath)
	if err != nil {
		return "", err
	}
	snippetCommands, err := snippets.New(settings.Snippets)
	if err != nil {
		return "", fmt.Errorf("%s: snippets: %w", pathutil.Shorten(configPath), err)
	}
	configuredModels, err := model.ParseRoundRobin(modelCachePath(), settings.Model.RoundRobin)
	if err != nil {
		return "", err
	}
	settings.Sandbox, err = shell.PreparePaths(settings.Sandbox, os.Stderr)
	if err != nil {
		return "", err
	}
	configuredRoots, err := shell.MountPaths(files, mode, settings.Sandbox)
	if err != nil {
		return "", err
	}
	defer shell.CloseRoots(configuredRoots)

	globalSkillDirs := append([]string{configDir("skills")}, settings.Skills.Include...)
	availableSkills, err := skill.Discover(workspaceDir, globalSkillDirs, os.Stderr)
	if err != nil {
		return "", err
	}

	availableSkills = skill.ExcludeGlobal(availableSkills, settings.Skills.Exclude)

	skillRoots, err := skill.MountGlobalSkills(files, availableSkills)
	if err != nil {
		return "", err
	}
	defer skill.Close(skillRoots)

	processes := sandbox.NewProcesses(args.Caps.Has(caps.Background))
	defer func() { _, _ = processes.Disable() }()

	providerName, modelName, effort, err := resolveProviderChoice(
		args.Provider,
		args.Model,
		args.Effort,
		configuredModels,
		modelRoundRobinPath(),
		resumedSession,
	)
	if err != nil {
		return "", err
	}
	choice, err := model.Chosen(modelCachePath(), providerName, modelName)
	if err != nil {
		return "", err
	}

	client, err := connect(choice, effort, os.Getenv(endpointVariable))
	if err != nil {
		return "", err
	}

	meta := store.Meta{
		Model:        modelName,
		WorkspaceDir: workspaceDir,
		Provider:     providerName,
		Effort:       effort,
	}

	log, err := openSession(resumedSession, meta)
	if err != nil {
		return "", err
	}
	defer func() { _ = log.Close() }()
	client.ObserveHTTP(log.Observer())

	tmpDir, err := openTmpDir(log.Name())
	if err != nil {
		return "", err
	}

	defer func() {
		if !log.Stored() {
			_ = os.Remove(tmpDir)
		}
	}()

	var systemPrompt string
	var contextFiles []prompt.File
	if resumedSession != nil && resumedSession.Meta.SystemPrompt != "" {
		systemPrompt = resumedSession.Meta.SystemPrompt
	} else {
		systemPrompt, contextFiles, err = prompt.Load(prompt.Config{
			GlobalPath:   globalContextPath(),
			Root:         root,
			WorkspaceDir: workspaceDir,
			SessionName:  log.Name(),
			TmpDir:       tmpDir,
			HomeDir:      homeDir,
			CurrentCaps:  args.Caps,
			ExtraPaths:   settings.Sandbox,
			Skills:       availableSkills,
		})
		if err != nil {
			return "", err
		}
	}

	if resumedSession == nil {
		meta.SystemPrompt = systemPrompt
		if err := log.SetMeta(meta); err != nil {
			return "", err
		}
	}

	tmpRoot, err := mountTmpDir(files, tmpDir)
	if err != nil {
		return "", err
	}
	defer func() { _ = tmpRoot.Close() }()

	snapshots := file.NewSnapshots()
	toolboxTools := toolbox.Rummage(files, snapshots)
	shellTool := shell.New(workspaceDir, homeDir, tmpDir, settings.Sandbox, mode, files, processes)

	toolboxTools = append(toolboxTools, shellTool)
	if notify.IsAvailable() {
		toolboxTools = append(toolboxTools, notify.New())
	}
	toolboxTools = truncate.Tools(toolboxTools)
	enabledTools := toolboxTools
	if len(args.Tools) > 0 {
		enabledTools, err = reduceTools(toolboxTools, args.Tools)
	}

	if err != nil {
		return "", err
	}

	systemCommands, err := commands.New(commands.Options{
		ConfigDir:  configDir(),
		StateDir:   stateDir(),
		ConfigFile: configPath,
		Editor:     settings.Editor,
		Output:     os.Stdout,
		Session: commands.Session{
			Name:      log.Name(),
			ID:        log.ID(),
			Directory: filepath.Join(sessionsDir(), log.Name()),
		},
	}, snippetCommands.Usages())
	if err != nil {
		return "", err
	}
	commandRegistry, err := slash.NewRegistry(systemCommands, snippetCommands)
	if err != nil {
		return "", err
	}

	chat := &Harness{
		agent:              agent.NewWithEnabledTools(systemPrompt, client, toolboxTools, enabledTools),
		screen:             output.New(os.Stdout).LinkPathsUnder(workspaceDir),
		terminal:           terminal.New(os.Stdout),
		metrics:            metrics.New(choice.ContextWindowTokens),
		commands:           commandRegistry,
		recorder:           recordSession(log),
		workspaceDir:       workspaceDir,
		mode:               mode,
		processes:          processes,
		getOnWithItMessage: settings.GetOnWithItMessage,
	}

	chat.segmentLayout, err = settings.BuildLayout(availableSegments(workspaceDir, log.Name(), modelName, effort, chat))
	if err != nil {
		return "", err
	}

	if err := settings.ValidateConsumed(); err != nil {
		return "", err
	}

	if resumedSession != nil {
		chat.restore(resumedSession)
	}

	projectSkills, globalSkills := skill.Counts(availableSkills)
	startupElapsed := startup.Elapsed()
	startupInfo := startup.Info{
		Session:       log.Name(),
		ContextFiles:  startup.FilesOf(contextFiles),
		ProjectSkills: projectSkills,
		GlobalSkills:  globalSkills,
		Snippets:      len(settings.Snippets),
		ToolBytes:     client.toolsSize(enabledTools),
	}
	if resumedSession == nil {
		chat.notify(startup.NewEvent(startupElapsed, startupInfo))
	}

	chat.begin(args.Message)

	if log.Stored() {
		fmt.Println(style.Subtle(resumeParams(log.Name())))
	}

	return "", nil
}

func reduceTools(availableTools []tool.Tool, enabledToolNames []string) ([]tool.Tool, error) {
	if len(enabledToolNames) == 0 {
		return availableTools, nil
	}

	availableNames := indexToolsByName(availableTools)
	enabledNames := make(map[string]struct{}, len(enabledToolNames))
	var unavailable []string
	for _, name := range enabledToolNames {
		if _, isEnabled := enabledNames[name]; isEnabled {
			continue
		}

		enabledNames[name] = struct{}{}
		if _, isAvailable := availableNames[name]; !isAvailable {
			unavailable = append(unavailable, name)
		}
	}
	if len(unavailable) > 0 {
		return nil, fmt.Errorf("tools not available: %s", strings.Join(unavailable, ", "))
	}

	tools := make([]tool.Tool, 0, len(enabledNames))
	for _, availableTool := range availableTools {
		if _, isEnabled := enabledNames[availableTool.Name()]; isEnabled {
			tools = append(tools, availableTool)
		}
	}

	return tools, nil
}

func indexToolsByName(tools []tool.Tool) map[string]tool.Tool {
	indexedTools := make(map[string]tool.Tool, len(tools))
	for _, availableTool := range tools {
		indexedTools[availableTool.Name()] = availableTool
	}

	return indexedTools
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
	configuredModels []model.Selection,
	roundRobinPath string,
	resumedSession *store.Session,
) (string, string, string, error) {
	if resumedSession != nil {
		sessionProvider := resumedSession.Meta.Provider
		if requestedProvider != "" && requestedProvider != sessionProvider {
			return "", "", "", fmt.Errorf("cannot resume a %s session with %s", sessionProvider, requestedProvider)
		}
		if requestedModel == "" {
			return sessionProvider, resumedSession.Meta.Model, resumedSession.Meta.Effort, nil
		}
	}

	if requestedModel != "" {
		return requestedProvider, requestedModel, requestedEffort, nil
	}

	selected, err := model.ReserveRoundRobin(roundRobinPath, configuredModels)
	if err != nil {
		return "", "", "", err
	}

	return selected.Provider, selected.Model, selected.Effort, nil
}

func connect(choice model.Choice, effort string, endpoint string) (*connection, error) {
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
	client, err := connect(model.Choice{Provider: providerName}, "", endpoint)
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

	storedSession, err := store.Read(sessionsDir(), name)
	if err != nil {
		return nil, err
	}
	if !storedSession.CanResume() {
		return nil, fmt.Errorf("session %s did not finish every turn and cannot be resumed safely (yet)", name)
	}
	return storedSession, nil
}

func openTmpDir(name string) (string, error) {
	tmp := tmpDir(name)

	if err := os.MkdirAll(tmp, 0o700); err != nil {
		return "", fmt.Errorf("could not prepare the tmp dir: %w", err)
	}

	return tmp, nil
}

func mountHomeDir(files *file.Root, homeDir string, mode *caps.Mode) (*os.Root, error) {
	homeRoot, err := os.OpenRoot(homeDir)
	if err != nil {
		return nil, fmt.Errorf("could not open the shell home: %w", err)
	}

	files.Mount(homeDir, file.New(homeRoot, caps.RefuseWrite(mode)))
	return homeRoot, nil
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
