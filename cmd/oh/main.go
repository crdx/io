package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/internal/file"
	"crdx.org/io/internal/sandbox"
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
	"crdx.org/io/cmd/oh/editor"
	"crdx.org/io/cmd/oh/location"
	"crdx.org/io/cmd/oh/menu"
	"crdx.org/io/cmd/oh/metrics"
	"crdx.org/io/cmd/oh/model"
	"crdx.org/io/cmd/oh/notification"
	"crdx.org/io/cmd/oh/onboarding"
	"crdx.org/io/cmd/oh/output"
	"crdx.org/io/cmd/oh/pathgrant"
	"crdx.org/io/cmd/oh/prompt"
	"crdx.org/io/cmd/oh/record"
	"crdx.org/io/cmd/oh/sessions"
	"crdx.org/io/cmd/oh/shell"
	"crdx.org/io/cmd/oh/skill"
	"crdx.org/io/cmd/oh/slash"
	"crdx.org/io/cmd/oh/startup"
	"crdx.org/io/cmd/oh/store"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/oh/terminal"
	"crdx.org/io/cmd/oh/textsizing"
	"crdx.org/io/cmd/oh/toolset"
	"crdx.org/io/cmd/oh/tty"
	"crdx.org/io/cmd/oh/usage"
	"crdx.org/io/cmd/oh/work"
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
	sandbox.Init()

	if cli.WriteCompletions(os.Stdout, os.Args[1:], cli.Sources{
		ModelCachePath: location.GetModelCachePath(os.Getenv(backend.EndpointVariable) != ""),
		SessionsDir:    location.GetSessionsDir(),
		ToolNames:      completableToolNames,
	}) {
		return
	}

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
		arguments := append([]string{self}, cli.InheritedOptions(os.Args[1:], transition.Kind)...)
		arguments = append(arguments, transition.Arguments...)
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

	isPromptPiped := !tty.Is(os.Stdin)

	if err := inputArgs.Check(isPromptPiped); err != nil {
		return "", err
	}

	keyboard, releaseKeyboard := tty.Keyboard(os.Stdin)
	defer releaseKeyboard()

	notices := io.Writer(os.Stdout)
	if inputArgs.IsPrinting {
		notices = os.Stderr
	}

	endpointURL := os.Getenv(backend.EndpointVariable)
	modelCachePath := location.GetModelCachePath(endpointURL != "")
	sessionsDir := location.GetSessionsDir()

	if inputArgs.Version {
		fmt.Println(cli.Version())
		return "", nil
	}

	if inputArgs.Login {
		err := onboarding.Login(inputArgs.LoginProvider, keyboard, os.Stdout)
		if errors.Is(err, onboarding.ErrCancelled) {
			return "", nil
		}
		return "", err
	}

	if inputArgs.Usage {
		return "", usage.Show(ctx, os.Stdout, usage.Options{JSON: inputArgs.JSON})
	}

	if inputArgs.List {
		return "", model.List(os.Stdout, modelCachePath)
	}

	workspace, err := work.Current()
	if err != nil {
		return "", err
	}

	workspaceDir := workspace.GetDir()

	if inputArgs.IsSessionPicker {
		return sessions.Choose(sessionsDir, workspace, keyboard, os.Stdout)
	}

	if _, err := onboarding.PrepareConfig(onboarding.Options{
		Input:          keyboard,
		Output:         os.Stdout,
		EndpointURL:    endpointURL,
		RequestedModel: inputArgs.Model,
		ResumedSession: inputArgs.Session,
		IsPrinting:     inputArgs.IsPrinting,
	}); err != nil {
		if errors.Is(err, onboarding.ErrCancelled) {
			return "", nil
		}

		return "", err
	}

	configPath := location.GetConfigFile()

	settings, configObserver, err := config.Observe(configPath)
	if err != nil {
		return "", err
	}
	defer configObserver.Close()

	editorConfiguration := editor.NewConfiguration(settings.Editor.Command)
	toolOutputLimit := truncate.NewLimit(settings.Tool.Output.Bytes)

	endpoints := backend.EndpointSettings{
		OverrideURL: endpointURL,
		OllamaHost:  settings.Provider.Ollama.Host,
	}
	listProviderModels := func(ctx context.Context, providerName string) ([]agent.Model, error) {
		return backend.ListModels(ctx, providerName, endpoints)
	}

	if inputArgs.Update {
		return "", model.Update(os.Stdout, endpointURL, modelCachePath, listProviderModels, inputArgs.IsShowingIgnored)
	}

	if err := model.Ensure(notices, endpointURL, modelCachePath, listProviderModels); err != nil {
		return "", err
	}

	if inputArgs.IsModelPicker {
		var chosenModel model.Selection
		var err error
		startup.Wait(func() { chosenModel, err = model.Choose(modelCachePath, backend.IsLoggedIn, keyboard, os.Stdout) })
		if errors.Is(err, menu.ErrCancelled) {
			return "", nil
		}
		if err != nil {
			return "", err
		}
		inputArgs.Model = chosenModel.String()
	}

	args, err := inputArgs.Parse(modelCachePath)
	if err != nil {
		return "", err
	}

	if err := sessions.ValidateFormats(sessionsDir); err != nil {
		return "", err
	}

	if err := sessions.RefreshListings(sessionsDir, notices); err != nil {
		return "", err
	}

	forkSource, err := sessions.GetForkSource(sessionsDir, workspace, args.SourceSession, args.Message)
	if err != nil {
		return "", err
	}
	if forkSource != nil {
		forkFile := startup.InitialFile{SourcePath: forkSource.InitialFilePath, DisplayName: forkSource.InitialFileName}
		args.AddedFiles = append([]startup.InitialFile{forkFile}, args.AddedFiles...)
		args.Message = forkSource.InitialUserMessage
	}

	resumedSession, err := sessions.LoadForResume(sessionsDir, workspace, args.Session)
	if err != nil {
		return "", err
	}

	args.Caps, err = sessions.OpeningCaps(args.Caps, args.WereCapsChosen, resumedSession)
	if err != nil {
		return "", err
	}

	args.Yolo, err = sessions.OpeningConfinement(args.Yolo, resumedSession)
	if err != nil {
		return "", err
	}

	if err := workspace.Validate(); err != nil {
		return "", err
	}

	if err := workspace.Open(); err != nil {
		return "", err
	}

	defer func() { _ = workspace.Close() }()

	if !args.Yolo {
		if err := shell.RequireSandbox(ctx); err != nil {
			return "", err
		}
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

	files := file.New(workspace.GetRoot(), caps.RefuseWrite(mode))
	homeRoot, err := shell.MountHomeDirectory(files, homeDir, mode)
	if err != nil {
		return "", err
	}
	defer func() { _ = homeRoot.Close() }()

	configuredModels, err := model.ParseRoundRobin(modelCachePath, settings.Model.RoundRobin)
	if err != nil {
		return "", err
	}
	settings.Sandbox, err = shell.PreparePaths(settings.Sandbox, os.Stderr)
	if err != nil {
		return "", err
	}
	pathAccess, err := shell.NewPathAccess(files, mode, settings.Sandbox)
	if err != nil {
		return "", err
	}
	defer pathAccess.Close()

	globalSkillDirs := append([]string{location.GetConfigDir("skills")}, settings.Skills.Include...)
	availableSkills, err := skill.Discover(workspace.GetDir(), globalSkillDirs, os.Stderr)
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
		args.Selection,
		sessions.ModelSelection(resumedSession),
		configuredModels,
		location.GetModelRoundRobinPath(),
	)
	if err != nil {
		startup.Wait(func() {
			selection, err = model.ChooseWhenNoneSelected(err, modelCachePath, backend.IsLoggedIn, keyboard, os.Stdout)
		})
	}
	if errors.Is(err, menu.ErrCancelled) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	choice, err := model.Chosen(modelCachePath, selection.Provider, selection.Model)
	if err != nil {
		return "", err
	}

	client, err := backend.Connect(choice, selection, endpoints)
	if err != nil {
		return "", err
	}

	meta := store.Meta{
		Model:        selection.Model,
		WorkspaceDir: workspaceDir,
		Provider:     selection.Provider,
		Effort:       selection.Effort,
		Yolo:         args.Yolo,
	}

	log, err := sessions.OpenWriter(sessionsDir, resumedSession, meta)
	if err != nil {
		return "", err
	}
	client.UseSession(log.ID())

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

	tmpDir, err := sessions.PrepareTemporaryDirectory(log.Name())
	if err != nil {
		return "", err
	}

	defer func() {
		if !log.IsPersisted() {
			_ = os.RemoveAll(tmpDir)
		}
	}()

	if isPromptPiped {
		pipedPrompt, err := startup.ReadPipedPrompt(os.Stdin)
		if err != nil {
			return "", err
		}
		args.Message = startup.JoinPrompt(args.Message, pipedPrompt)
	}

	if len(args.AddedFiles) > 0 {
		initialFilesMessage, err := startup.PrepareInitialFiles(args.AddedFiles, tmpDir)
		if err != nil {
			return "", err
		}
		args.Message = startup.JoinPrompt(initialFilesMessage, args.Message)
	}

	var systemPrompt string
	if resumedSession != nil && resumedSession.Meta.SystemPrompt != "" {
		systemPrompt = resumedSession.Meta.SystemPrompt
	} else {
		systemPrompt, _, err = prompt.Load(prompt.Config{
			GlobalPath:  location.GetGlobalContextPath(),
			Workspace:   workspace,
			SessionName: log.Name(),
			TmpDir:      tmpDir,
			HomeDir:     homeDir,
			CurrentCaps: args.Caps,
			ExtraPaths:  settings.Sandbox,
			Skills:      availableSkills,
			Yolo:        args.Yolo,
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

	tmpRoot, err := shell.MountTemporaryDirectory(files, tmpDir)
	if err != nil {
		return "", err
	}
	defer func() { _ = tmpRoot.Close() }()

	pathGrants := pathgrant.New(workspace, pathAccess)
	var pathGrantRestoreResult pathgrant.RestoreResult
	if resumedSession != nil {
		if recordedGrants, found := pathgrant.LastRecorded(resumedSession.Events); found {
			pathGrants, pathGrantRestoreResult = pathgrant.NewRestored(workspace, pathAccess, recordedGrants)
		}
	}

	screen := output.New(os.Stdout).LinkPathsUnder(workspace.GetDir())
	if args.IsPrinting {
		screen.AppendOnly()
	} else {
		screen.SetTextSizingSupported(textsizing.Detect(keyboard, os.Stdout))
	}

	snapshots := file.NewSnapshots()
	toolboxTools := toolbox.Rummage(files, snapshots)
	shellTool := shell.New(workspace.GetDir(), homeDir, tmpDir, pathAccess, mode, files, args.Yolo)

	toolboxTools = append(toolboxTools, shellTool)
	if notify.IsAvailable() {
		toolboxTools = append(toolboxTools, notify.New(screen.WriteEscape))
	}
	toolboxTools = append(toolboxTools, title.New())
	toolboxTools = append(toolboxTools, web.New(func() bool {
		return mode.Current().Has(caps.Web)
	}, client.Search)...)
	toolboxTools = truncate.Tools(toolboxTools, toolOutputLimit)

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
		Workspace:        workspace,
		ScratchDir:       tmpDir,
		HomeDir:          homeDir,
		Editor:           editorConfiguration,
		Output:           os.Stdout,
		PathGrants: commands.PathGrants{
			Grant:      pathGrants.Grant,
			Revoke:     pathGrants.Revoke,
			GetCurrent: pathGrants.GetCurrent,
		},
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
				transition, err = cycle.ForkedSessionTransition(
					start.ModelGlob,
					selection.Effort,
					model.Choices(modelCachePath),
					start.SourceSessionName,
				)
			} else {
				transition, err = cycle.NewSessionTransition(
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

	chat = &App{
		agent:               agent.NewWithEnabledTools(systemPrompt, client, toolboxTools, enabledTools),
		screen:              screen,
		terminal:            terminal.New(os.Stdout, workspace),
		metrics:             metrics.New(choice.ContextWindowTokens),
		recorder:            record.New(log),
		editorConfiguration: editorConfiguration,
		toolOutputLimit:     toolOutputLimit,
		workspace:           workspace,
		mode:                mode,
		pathGrants:          pathGrants,
		configObserver:      configObserver,
		startedAt:           time.Now(),
		keyboard:            keyboard,
		isPrinting:          args.IsPrinting,
		isYolo:              args.Yolo,
	}
	if resumedSession == nil && model.SupportsFastMode(selection.Provider) {
		chat.openingEvents = []agent.Event{model.FastModeEvent(selection.IsFast)}
	}
	chat.onFailure = func(failure error) {
		_ = notification.SendTurnError(context.Background(), screen.WriteEscape, workspace, failure)
	}

	usageReporter, _ := client.Client.(agent.UsageReporter)

	barRegistry := bar.NewRegistry(bar.Options{
		Workspace:             workspace,
		CurrentSessionName:    log.Name(),
		ModelName:             selection.Model,
		ModelEffort:           selection.Effort,
		ModelEffortLevels:     choice.EffortLevels,
		IsFast:                selection.IsFast,
		UsageReporter:         usageReporter,
		UsageCachePath:        location.GetUsageCachePath(selection.Provider, endpointURL != ""),
		UsageIsSelfRefreshing: backend.RefreshesOwnUsage(selection.Provider),
		UsageGauges:           usage.TerminalGauges(keyboard, os.Stdout),
		Sources:               chat.getBarSources(),
	})
	liveSettings, err := settings.BuildLive(barRegistry)
	if err != nil {
		return "", err
	}
	commandRegistry, err := slash.NewRegistry(systemCommands, liveSettings.SnippetCommandSet)
	if err != nil {
		return "", err
	}
	chat.commands = commandRegistry
	chat.continueMessage = liveSettings.ContinueMessage
	chat.streamingMode = liveSettings.StreamingMode
	chat.barConfiguration = bar.NewConfiguration(barRegistry, liveSettings.SegmentLayout)

	if resumedSession != nil {
		chat.restore(resumedSession)
	}
	for _, failure := range pathGrantRestoreResult.Failures {
		correction, err := pathgrant.ChangeEvent(failure.Grant.Path, pathGrants.GetCurrent())
		if err != nil {
			return "", err
		}
		chat.queuePathGrantChange(correction)
		chat.notifyFailure(fmt.Sprintf(
			"Temporary access to %s could not be restored: %v",
			failure.Grant.Path,
			failure.Err,
		))
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
		_, _ = fmt.Fprintln(notices, style.Subtle(sessions.ResumeCommand(os.Args[0], log.Name())))
	}

	return "", nil
}
