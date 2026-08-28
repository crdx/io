package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"syscall"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/internal/file"
	"crdx.org/io/internal/sandbox"
	"crdx.org/io/internal/util/pathutil"
	"crdx.org/io/tool/middleware/truncate"
	"crdx.org/io/toolbox"
	"crdx.org/io/toolbox/notify"
	"crdx.org/io/toolbox/title"
	"crdx.org/io/toolbox/web"

	"crdx.org/io/cmd/oh/backend"
	"crdx.org/io/cmd/oh/bar"
	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/cli"
	"crdx.org/io/cmd/oh/commands"
	"crdx.org/io/cmd/oh/config"
	"crdx.org/io/cmd/oh/cycle"
	"crdx.org/io/cmd/oh/location"
	"crdx.org/io/cmd/oh/metrics"
	"crdx.org/io/cmd/oh/model"
	"crdx.org/io/cmd/oh/output"
	"crdx.org/io/cmd/oh/prompt"
	"crdx.org/io/cmd/oh/record"
	"crdx.org/io/cmd/oh/sessions"
	"crdx.org/io/cmd/oh/shell"
	"crdx.org/io/cmd/oh/skill"
	"crdx.org/io/cmd/oh/slash"
	"crdx.org/io/cmd/oh/snippets"
	"crdx.org/io/cmd/oh/startup"
	"crdx.org/io/cmd/oh/store"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/oh/terminal"
	"crdx.org/io/cmd/oh/toolset"
	"crdx.org/io/cmd/oh/workspace"
)

var completableToolNames = []string{
	"read",
	"ls",
	"find",
	"grep",
	"write",
	"edit",
	"bash",
	"notify",
	title.Name,
	"web_search",
	"web_fetch",
}

func main() {
	if cli.WriteCompletions(os.Stdout, os.Args[1:], cli.Sources{
		ModelCachePath: location.GetModelCachePath(os.Getenv(backend.EndpointVariable) != ""),
		SessionsDir:    location.GetSessionsDir(),
		ToolNames:      completableToolNames,
	}) {
		return
	}

	sandbox.Init()

	style.Init(os.Stdout)

	hooks := cycle.NewHooks(func(err error) { fmt.Fprintln(os.Stderr, "session hook:", err) })
	transition := cycle.Transition{}
	chosenSession, err := run(hooks, &transition)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if chosenSession != "" {
		transition = cycle.Transition{Kind: cycle.ResumeSession, Arguments: []string{"-r", chosenSession}}
	}
	if transition.Kind == cycle.Quit {
		return
	}

	terminal.ResetScrollback(os.Stdout)

	self, err := os.Executable()
	if err == nil {
		arguments := append([]string{self}, transition.Arguments...)
		err = syscall.Exec(self, arguments, os.Environ()) //nolint:gosec // re-executing the binary itself
	}

	fmt.Fprintln(os.Stderr, "could not open the session:", err)
	os.Exit(1)
}

//nolint:gocyclo // lol no
func run(hooks *cycle.Hooks, requestedTransition *cycle.Transition) (string, error) {
	ctx := context.Background()
	var sessionInfo cycle.Session
	hasStarted := false
	stopReason := cycle.StoppedByFailure
	defer func() {
		if hasStarted {
			hooks.EmitSessionStopped(ctx, cycle.SessionStopped{Session: sessionInfo, Reason: stopReason})
		}
	}()

	inputArgs := cli.Bind()
	endpointURL := os.Getenv(backend.EndpointVariable)
	modelCachePath := location.GetModelCachePath(endpointURL != "")
	sessionsDir := location.GetSessionsDir()

	if inputArgs.Version {
		fmt.Println(version())
		return "", nil
	}

	if inputArgs.List {
		return "", model.List(os.Stdout, modelCachePath)
	}

	if inputArgs.IsSessionPicker {
		return sessions.Choose(sessionsDir, inputArgs.WorkspaceDir, os.Stdin, os.Stdout)
	}

	configPath := location.GetConfigFile()
	settings, configObserver, err := config.Observe(configPath)
	if err != nil {
		return "", err
	}
	defer configObserver.Close()

	endpoints := backend.EndpointSettings{
		OverrideURL: endpointURL,
		OllamaHost:  settings.Provider.Ollama.Host,
	}
	listProviderModels := func(ctx context.Context, providerName string) ([]agent.Model, error) {
		return backend.ListModels(ctx, providerName, endpoints)
	}

	if inputArgs.Update {
		return "", model.Update(os.Stdout, endpointURL, modelCachePath, listProviderModels)
	}

	if err := model.Ensure(os.Stdout, endpointURL, modelCachePath, listProviderModels); err != nil {
		return "", err
	}

	args, err := inputArgs.Parse(modelCachePath)
	if err != nil {
		return "", err
	}

	if err := sessions.ValidateFormats(sessionsDir); err != nil {
		return "", err
	}

	forkSource, err := sessions.GetForkSource(sessionsDir, args.SourceSession, args.Message)
	if err != nil {
		return "", err
	}
	if forkSource != nil {
		args.WorkspaceDir = forkSource.WorkspaceDir
		args.AddedFiles = append([]string{forkSource.InitialFilePath}, args.AddedFiles...)
		args.Message = forkSource.InitialUserMessage
	}

	resumedSession, err := sessions.LoadForResume(sessionsDir, args.Session)
	if err != nil {
		return "", err
	}
	if resumedSession != nil {
		args.WorkspaceDir = resumedSession.Meta.WorkspaceDir
	}

	args.Caps, err = sessions.OpeningCaps(args.Caps, args.WereCapsChosen, resumedSession)
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
	if err := workspace.Validate(workspaceDir); err != nil {
		return "", err
	}

	homeDir := location.GetShellHomeDir()
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

	snippetCommands, err := snippets.New(settings.Snippets)
	if err != nil {
		return "", fmt.Errorf("%s: snippets: %w", pathutil.Shorten(configPath), err)
	}
	configuredModels, err := model.ParseRoundRobin(modelCachePath, settings.Model.RoundRobin)
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

	globalSkillDirs := append([]string{location.GetConfigDir("skills")}, settings.Skills.Include...)
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

	selection, err := backend.Resolve(
		model.Selection{Provider: args.Provider, Model: args.Model, Effort: args.Effort},
		resumedSelection(resumedSession),
		configuredModels,
		location.GetModelRoundRobinPath(),
	)
	if err != nil {
		return "", err
	}
	choice, err := model.Chosen(modelCachePath, selection.Provider, selection.Model)
	if err != nil {
		return "", err
	}

	client, err := backend.Connect(choice, selection.Effort, endpoints)
	if err != nil {
		return "", err
	}

	meta := store.Meta{
		Model:        selection.Model,
		WorkspaceDir: workspaceDir,
		Provider:     selection.Provider,
		Effort:       selection.Effort,
	}

	log, err := sessions.OpenWriter(sessionsDir, resumedSession, meta)
	if err != nil {
		return "", err
	}
	client.UseSession(log.Name())

	sessionInfo = cycle.Session{
		Name:         log.Name(),
		ID:           log.ID(),
		Directory:    filepath.Join(sessionsDir, log.Name()),
		WorkspaceDir: workspaceDir,
		Provider:     selection.Provider,
		Model:        selection.Model,
		Effort:       selection.Effort,
	}
	defer func() { _ = log.Close() }()
	hooks.EmitSessionStarting(ctx, cycle.SessionStarting{Session: sessionInfo})
	client.ObserveHTTP(log.Observer())

	tmpDir, err := openTmpDir(log.Name())
	if err != nil {
		return "", err
	}

	defer func() {
		if !log.IsPersisted() {
			_ = os.RemoveAll(tmpDir)
		}
	}()

	if len(args.AddedFiles) > 0 {
		initialFilesMessage, err := startup.PrepareInitialFiles(args.AddedFiles, tmpDir)
		if err != nil {
			return "", err
		}
		if args.Message == "" {
			args.Message = initialFilesMessage
		} else {
			args.Message = initialFilesMessage + "\n\n" + args.Message
		}
	}

	var systemPrompt string
	if resumedSession != nil && resumedSession.Meta.SystemPrompt != "" {
		systemPrompt = resumedSession.Meta.SystemPrompt
	} else {
		systemPrompt, _, err = prompt.Load(prompt.Config{
			GlobalPath:   location.GetGlobalContextPath(),
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
	shellTool := shell.New(workspaceDir, homeDir, tmpDir, settings.Sandbox, mode, files)

	toolboxTools = append(toolboxTools, shellTool)
	if notify.IsAvailable() {
		toolboxTools = append(toolboxTools, notify.New())
	}
	toolboxTools = append(toolboxTools, title.New())
	toolboxTools = append(toolboxTools, web.New(func() bool {
		return mode.Current().Has(caps.Web)
	}, client.Search)...)
	toolboxTools = truncate.Tools(toolboxTools)

	enabledTools, err := toolset.Reduce(toolboxTools, args.Tools)
	if err != nil {
		return "", err
	}

	var chat *App
	systemCommands, err := commands.New(commands.Options{
		ConfigDir:        location.GetConfigDir(),
		ConfigFile:       configPath,
		SystemPromptFile: location.GetGlobalContextPath(),
		SkillDirs:        globalSkillDirs,
		WorkspaceDir:     workspaceDir,
		ScratchDir:       tmpDir,
		HomeDir:          homeDir,
		Editor:           settings.Editor.Command,
		Output:           os.Stdout,
		Session: commands.Session{
			Name:           log.Name(),
			ID:             log.ID(),
			Directory:      filepath.Join(sessionsDir, log.Name()),
			IsPersisted:    log.IsPersisted,
			GetLastMessage: func() (string, bool) { return chat.getLastMessage() },
		},
		StartSession: func(start commands.SessionStart) error {
			var transition cycle.Transition
			var err error
			if start.SourceSessionName != "" {
				transition, err = forkedSessionTransition(
					start.ModelGlob,
					selection.Effort,
					model.Choices(modelCachePath),
					start.SourceSessionName,
				)
			} else {
				transition, err = newSessionTransition(
					workspaceDir,
					start.ModelGlob,
					selection.Effort,
					model.Choices(modelCachePath),
				)
			}
			if err != nil {
				return err
			}
			return chat.requestTransition(transition)
		},
	})
	if err != nil {
		return "", err
	}
	commandRegistry, err := slash.NewRegistry(systemCommands, snippetCommands)
	if err != nil {
		return "", err
	}

	chat = &App{
		agent:          agent.NewWithEnabledTools(systemPrompt, client, toolboxTools, enabledTools),
		screen:         output.New(os.Stdout).LinkPathsUnder(workspaceDir),
		terminal:       terminal.New(os.Stdout, workspaceDir),
		metrics:        metrics.New(choice.ContextWindowTokens),
		commands:       commandRegistry,
		recorder:       record.New(log),
		workspaceDir:   workspaceDir,
		mode:           mode,
		configObserver: configObserver,
		startedAt:      time.Now(),
	}

	usageReporter, _ := client.Client.(agent.UsageReporter)

	barRegistry := bar.NewRegistry(bar.Options{
		WorkspaceDir:       workspaceDir,
		CurrentSessionName: log.Name(),
		ModelName:          selection.Model,
		ModelEffort:        selection.Effort,
		ModelEffortLevels:  choice.EffortLevels,
		UsageReporter:      usageReporter,
		UsageCachePath:     location.GetUsageCachePath(selection.Provider, endpointURL != ""),
		Sources:            chat.getBarSources(),
	})
	liveSettings, err := settings.BuildLive(barRegistry)
	if err != nil {
		return "", err
	}
	chat.getOnWithItMessage = liveSettings.GetOnWithItMessage
	chat.barConfiguration = bar.NewConfiguration(barRegistry, liveSettings.SegmentLayout)

	if resumedSession != nil {
		chat.restore(resumedSession)
	}

	projectSkills, globalSkills := skill.Counts(availableSkills)
	startupElapsed := startup.Elapsed()
	startupInfo := startup.Info{
		Session:       log.Name(),
		PromptBytes:   len(systemPrompt),
		ProjectSkills: projectSkills,
		GlobalSkills:  globalSkills,
		Snippets:      len(settings.Snippets),
		ToolBytes:     client.ToolsSize(enabledTools),
	}
	if resumedSession == nil {
		chat.notify(startup.NewEvent(startupElapsed, startupInfo))
	}

	hasStarted = true
	hooks.EmitSessionStarted(ctx, cycle.SessionStarted{Session: sessionInfo})
	transition := chat.begin(args.Message)
	*requestedTransition = transition
	stopReason = transition.StopReason()
	hooks.EmitSessionStopping(ctx, cycle.SessionStopping{Session: sessionInfo, Reason: stopReason})

	if log.IsPersisted() && transition.Kind == cycle.Quit {
		fmt.Println(style.Subtle(sessions.ResumeCommand(os.Args[0], log.Name())))
	}

	return "", nil
}

func resumedSelection(resumedSession *store.Session) model.Selection {
	if resumedSession == nil {
		return model.Selection{}
	}

	return model.Selection{
		Provider: resumedSession.Meta.Provider,
		Model:    resumedSession.Meta.Model,
		Effort:   resumedSession.Meta.Effort,
	}
}

func openTmpDir(name string) (string, error) {
	tmp := location.GetTmpDir(name)

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

func version() string {
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" {
		return "unknown"
	}

	return info.Main.Version
}
