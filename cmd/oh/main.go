package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/internal/file"
	"crdx.org/io/internal/jobs"
	"crdx.org/io/internal/money"
	"crdx.org/io/internal/sandbox"
	"crdx.org/io/internal/sandbox/keeper"
	"crdx.org/io/internal/util"
	"crdx.org/io/tool"
	"crdx.org/io/tool/middleware/truncate"
	"crdx.org/io/toolbox"
	"crdx.org/io/toolbox/expose"
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
	"crdx.org/io/cmd/oh/demo"
	"crdx.org/io/cmd/oh/drops"
	"crdx.org/io/cmd/oh/editor"
	"crdx.org/io/cmd/oh/graphics"
	"crdx.org/io/cmd/oh/location"
	"crdx.org/io/cmd/oh/menu"
	"crdx.org/io/cmd/oh/metrics"
	"crdx.org/io/cmd/oh/model"
	"crdx.org/io/cmd/oh/notification"
	"crdx.org/io/cmd/oh/onboarding"
	"crdx.org/io/cmd/oh/output"
	"crdx.org/io/cmd/oh/pathgrant"
	"crdx.org/io/cmd/oh/pictures"
	"crdx.org/io/cmd/oh/portgrant"
	"crdx.org/io/cmd/oh/prompt"
	"crdx.org/io/cmd/oh/record"
	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/sessions"
	"crdx.org/io/cmd/oh/shell"
	"crdx.org/io/cmd/oh/skill"
	"crdx.org/io/cmd/oh/slash"
	"crdx.org/io/cmd/oh/startup"
	"crdx.org/io/cmd/oh/store"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/oh/terminal"
	"crdx.org/io/cmd/oh/textsizing"
	"crdx.org/io/cmd/oh/toolresult"
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
	"job",
	"notify",
	title.Name,
	"web_search",
	"web_fetch",
}

func main() {
	sandbox.Init()

	if request, isRequested, err := toolresult.ParseRequest(os.Args[1:]); isRequested {
		style.Init(os.Stdout)
		if err == nil {
			err = toolresult.Show(request.URL, request.ShouldPage)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

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

func isTerminalLocal() bool {
	return os.Getenv("SSH_CLIENT") == "" && os.Getenv("SSH_TTY") == "" && os.Getenv("SSH_CONNECTION") == ""
}

func getConfigSources(workspaceDir string) []config.Source {
	return []config.Source{
		{Path: location.GetConfigFile()},
		{Path: filepath.Join(workspaceDir, "oh.toml"), IsOverride: true},
	}
}

func configuredRotation(settings config.Config, isSimulated bool) []string {
	if isSimulated {
		return nil
	}

	return settings.Model.RoundRobin
}

func applyDefaultCaps(options *cli.Options, settings config.Config) {
	if !options.WereCapsChosen {
		options.Caps = caps.Set(settings.Caps.Default)
	}
}

func ensureCurrency(ctx context.Context, output io.Writer, code string, isSimulated bool) money.Currency {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" || code == money.DollarCode || isSimulated {
		return money.Dollar()
	}

	path := location.GetExchangeRateCachePath()

	if err := money.Ensure(ctx, "", path, code); err != nil {
		_, _ = fmt.Fprintln(output, style.Change("exchange rate not refreshed: %s", err))
	}

	return money.Load(path, code)
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

	configSources := getConfigSources(workspaceDir)
	configPath := configSources[0].Path
	isSimulated := inputArgs.IsDemoing

	if !isSimulated {
		_, isChosen, err := onboarding.PrepareConfig(onboarding.Options{
			Input:          keyboard,
			Output:         os.Stdout,
			EndpointURL:    endpointURL,
			RequestedModel: inputArgs.Model,
			ResumedSession: inputArgs.Session,
			ConfigSources:  configSources,
			IsPrinting:     inputArgs.IsPrinting,
		})
		if err != nil {
			if errors.Is(err, onboarding.ErrCancelled) {
				return "", nil
			}

			return "", err
		}

		isSimulated = isChosen
	}

	if isSimulated {
		simulation, err := demo.Start()
		if err != nil {
			return "", err
		}
		defer simulation.Close()

		endpointURL = simulation.EndpointURL
		inputArgs.Model = simulation.Selection
		modelCachePath = location.GetModelCachePath(true)
		sessionsDir = location.GetSessionsDir()
	}

	settings, configObserver, err := config.ObserveSources(configSources...)
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

	currency := ensureCurrency(ctx, notices, settings.Ui.Currency, isSimulated)

	if inputArgs.IsModelPicker {
		var chosenModel model.Selection
		var err error
		startup.Wait(func() {
			chosenModel, err = model.Choose(modelCachePath, currency, backend.IsLoggedIn, keyboard, os.Stdout)
		})
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
	applyDefaultCaps(&args, settings)

	if err := sessions.ValidateStoredFormats(sessionsDir); err != nil {
		return "", err
	}

	if err := sessions.RefreshListing(sessionsDir, notices, args.Session); err != nil {
		return "", err
	}

	forkSource, err := sessions.GetForkSource(sessionsDir, workspace, args.SourceSession, args.Message)
	if err != nil {
		return "", err
	}
	if forkSource != nil {
		args.Message = forkSource.GetInitialUserMessage(forkSource.DroppedChatName)
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

	configuredModels, err := model.ParseRoundRobin(modelCachePath, configuredRotation(settings, isSimulated))
	if err != nil {
		return "", err
	}
	settings.Sandbox, err = shell.PreparePaths(settings.Sandbox, os.Stderr)
	if err != nil {
		return "", err
	}
	hostLoopback := sessions.OpeningHostLoopback(settings.Sandbox.HostLoopback, resumedSession)
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

	selectionTime := time.Now()
	selection, err := backend.ResolveAvailable(
		args.Selection,
		sessions.ModelSelection(resumedSession),
		configuredModels,
		location.GetModelRoundRobinPath(),
		func(candidate model.Selection) bool {
			return usage.IsSelectionAvailable(
				location.GetUsageCachePath(candidate.Provider, endpointURL != ""),
				candidate.Model,
				selectionTime,
			)
		},
	)
	if err != nil {
		startup.Wait(func() {
			selection, err = model.ChooseWhenNoneSelected(
				err, modelCachePath, currency, backend.IsLoggedIn, keyboard, os.Stdout,
			)
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
		IsFast:       selection.IsFast,
		HostLoopback: slices.Clone(hostLoopback),
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

	closeDrops := func() error { return nil }
	areDropsMounted := false
	mountDrops := func(shouldExist bool) error {
		if areDropsMounted {
			return nil
		}

		nextCloseDrops, areNewDropsMounted, err := drops.Mount(files, sessionInfo.Directory)
		if err != nil {
			return err
		}
		if shouldExist && !areNewDropsMounted {
			return errors.New("the drops directory disappeared")
		}
		if areNewDropsMounted {
			closeDrops = nextCloseDrops
			areDropsMounted = true
		}
		return nil
	}
	if err := mountDrops(false); err != nil {
		return "", fmt.Errorf("mount drops: %w", err)
	}
	defer func() { _ = closeDrops() }()

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

	sandboxRunner, jobManager, keeperProcess, closeKeeper, keeperRefusal := openRunner(ctx, args.Yolo, hostLoopback)
	defer closeKeeper()

	if jobManager != nil {
		defer func() { _ = jobManager.Close() }()
	}
	if keeperRefusal != nil {
		_, _ = fmt.Fprintln(notices, style.Change(
			"background jobs and inter-command networking are unavailable: "+keeperRefusal.Error(),
		))
	}

	var systemPrompt string
	if resumedSession != nil && resumedSession.Meta.SystemPrompt != "" {
		systemPrompt = resumedSession.Meta.SystemPrompt
	} else {
		systemPrompt, _, err = prompt.Load(prompt.Config{
			GlobalPath:     location.GetGlobalContextPath(),
			Workspace:      workspace,
			SessionName:    log.Name(),
			TmpDir:         tmpDir,
			HomeDir:        homeDir,
			CurrentCaps:    args.Caps,
			ExtraPaths:     settings.Sandbox,
			DropsDirectory: drops.GetDirectory(sessionInfo.Directory),
			Skills:         availableSkills,
			JobsGranted:    jobManager != nil,
			Yolo:           args.Yolo,
		})
		if err != nil {
			return "", err
		}
	}
	systemPrompt = prompt.WithDropsDirectory(systemPrompt, drops.GetDirectory(sessionInfo.Directory))

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

	hostToSandboxAddress := portgrant.AddressFor(log.Name())
	hostToSandboxHostname := settings.Ports.GetHostname(log.Name(), hostToSandboxAddress)
	hostToSandboxExposer := newHostToSandboxExposer(ctx, keeperProcess, hostToSandboxAddress)
	var hostToSandbox *portgrant.HostToSandbox
	sandboxToHostExposer := newSandboxToHostExposer(ctx, keeperProcess)
	sandboxToHost := portgrant.NewSandboxToHost(sandboxToHostExposer, func() []uint16 {
		if hostToSandbox == nil {
			return nil
		}
		return hostToSandbox.GetCurrent()
	})
	var sandboxToHostRestoreResult portgrant.SandboxToHostRestoreResult
	if resumedSession != nil {
		if recordedPorts, found := portgrant.LastRecordedSandboxToHost(resumedSession.Events); found {
			sandboxToHost, sandboxToHostRestoreResult = portgrant.NewRestoredSandboxToHost(
				sandboxToHostExposer,
				func() []uint16 {
					if hostToSandbox == nil {
						return nil
					}
					return hostToSandbox.GetCurrent()
				},
				recordedPorts,
			)
		}
	}

	hostToSandbox = portgrant.NewHostToSandbox(hostToSandboxExposer, hostToSandboxHostname, hostLoopback)
	hostToSandbox.SetSandboxToHostPorts(sandboxToHost.GetCurrent)
	var hostToSandboxRestoreResult portgrant.HostToSandboxRestoreResult
	if resumedSession != nil {
		if recordedPorts, found := portgrant.LastRecordedHostToSandbox(resumedSession.Events); found {
			hostToSandbox, hostToSandboxRestoreResult = portgrant.NewRestoredHostToSandbox(
				hostToSandboxExposer, hostToSandboxHostname, hostLoopback, recordedPorts,
			)
			hostToSandbox.SetSandboxToHostPorts(sandboxToHost.GetCurrent)
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
	shellTool := shell.New(workspace.GetDir(), homeDir, tmpDir, pathAccess, mode, files, args.Yolo, sandboxRunner)

	toolboxTools = append(toolboxTools, shellTool)

	if jobManager != nil {
		toolboxTools = append(toolboxTools, shell.NewJob(
			jobManager, workspace.GetDir(), homeDir, tmpDir, pathAccess, mode, files, args.Yolo,
		))
	}
	if keeperProcess != nil {
		toolboxTools = append(toolboxTools, expose.New(hostToSandbox.ForModel()))
	}
	if notify.IsAvailable() {
		toolboxTools = append(toolboxTools, notify.New(screen.WriteEscape))
	}
	toolboxTools = append(toolboxTools, title.New())
	toolboxTools = append(toolboxTools, web.New(func() bool {
		return mode.Current().Has(caps.Web)
	}, client.Search)...)
	toolboxTools = truncate.Tools(toolboxTools, toolOutputLimit)

	enabledToolNames := args.Tools
	if resumedSession != nil && len(resumedSession.Meta.Tools) > 0 {
		var absentToolNames []string
		enabledToolNames, absentToolNames = toolset.Partition(toolboxTools, resumedSession.Meta.Tools)
		if len(absentToolNames) > 0 {
			_, _ = fmt.Fprintln(notices, style.Change(
				"tools used by this conversation are no longer offered, so its prompt cache will be "+
					"rebuilt: "+strings.Join(absentToolNames, ", "),
			))
		}
	}

	enabledTools, err := toolset.Reduce(toolboxTools, enabledToolNames)
	if err != nil {
		return "", err
	}

	if resumedSession == nil {
		meta.SystemPrompt = systemPrompt
		meta.Tools = toolset.Names(enabledTools)
		if err := log.SetMeta(meta); err != nil {
			return "", err
		}
	}

	if forkSource != nil {
		transcriptPath, err := forkSource.CopyChat(sessionInfo.Directory, log.EnsurePersisted)
		if err != nil {
			return "", fmt.Errorf("copy the forked session chat: %w", err)
		}
		if err := mountDrops(true); err != nil {
			_ = os.Remove(transcriptPath)
			return "", fmt.Errorf("make the forked session chat readable: %w", err)
		}
		args.Message = forkSource.GetMessageWithChatAt(args.Message, transcriptPath)
	}

	var app *App
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
			Grant: pathGrants.Grant,
			Revoke: func(path string) (agent.Event, error) {
				event, err := pathGrants.Revoke(path)
				if err != nil {
					return event, err
				}
				app.stopJobsHoldingPath(path)

				return event, nil
			},
			GetCurrent: pathGrants.GetCurrent,
		},
		HostToSandbox: commands.HostToSandbox{
			Hide:       hostToSandbox.Hide,
			GetCurrent: hostToSandbox.GetCurrent,
			GetURL:     hostToSandbox.URL,
		},
		SandboxToHost: commands.SandboxToHost{
			Expose:     sandboxToHost.Expose,
			Revoke:     sandboxToHost.Revoke,
			GetCurrent: sandboxToHost.GetCurrent,
		},
		Jobs: managedJobs(jobManager),
		GetInfo: func() (string, error) {
			return app.display.bar.RenderInfo(segment.Context{})
		},
		Session: commands.Session{
			Name:           log.Name(),
			ID:             log.ID(),
			Directory:      filepath.Join(sessionsDir, log.Name()),
			IsPersisted:    log.IsPersisted,
			GetLastMessage: func() (string, bool) { return app.getLastMessage() },
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
			return app.requestTransition(transition)
		},
	})
	if err != nil {
		return "", err
	}

	usageCachePath := location.GetUsageCachePath(selection.Provider, endpointURL != "")
	providerClient := usage.Guard(ctx, client.Client, usage.GuardSettings{
		ProviderName: model.ProviderName(selection.Provider),
		ModelName:    selection.Model,
		CachePath:    usageCachePath,
		Now:          time.Now,
	})

	app = &App{
		agent:    agent.NewWithEnabledTools(systemPrompt, providerClient, toolboxTools, enabledTools),
		screen:   screen,
		terminal: terminal.New(os.Stdout, workspace),
		metrics: metrics.New(metrics.Settings{
			ContextWindowTokens: choice.ContextWindowTokens,
			Prices:              choice.Prices,
		}),
		recorder:        record.New(log),
		editorConfig:    editorConfiguration,
		toolOutputLimit: toolOutputLimit,
		workspace:       workspace,
		mode:            mode,
		pathGrants:      pathGrants,
		hostToSandbox:   hostToSandbox,
		sandboxToHost:   sandboxToHost,
		jobs:            jobState{manager: jobManager},
		configObserver:  configObserver,
		runMode:         runMode{isPrinting: args.IsPrinting, isYolo: args.Yolo},
		startedAt:       util.WallClock(time.Now()),
		keyboard:        keyboard,
	}
	if resumedSession == nil && model.SupportsFastMode(selection.Provider) {
		app.openingEvents = []agent.Event{model.FastModeEvent(selection.IsFast)}
	}
	app.onFailure = func(failure error) {
		_ = notification.SendTurnError(context.Background(), screen.WriteEscape, workspace, failure)
	}
	app.savePastedImage = func(mediaType string, data []byte) (string, error) {
		path, err := drops.SaveImage(sessionInfo.Directory, log.EnsurePersisted, mediaType, data)
		if err != nil {
			return "", err
		}
		if err := mountDrops(true); err != nil {
			_ = os.Remove(path)
			return "", fmt.Errorf("make the pasted image readable: %w", err)
		}
		return path, nil
	}

	if cellWidth, cellHeight, hasGraphics := graphics.Detect(keyboard, os.Stdout); hasGraphics {
		app.display.pictures = pictureDisplay{
			sessionDirectory: sessionInfo.Directory,
			cellWidth:        cellWidth,
			cellHeight:       cellHeight,
			isLocal:          isTerminalLocal(),
		}

		app.agent.StorePicturesWith(func(picture tool.Image) *agent.Picture {
			reference, err := pictures.Store(sessionInfo.Directory, log.EnsurePersisted, picture)
			if err != nil {
				return nil
			}

			return reference
		})
	}

	usageReporter, _ := client.Client.(agent.UsageReporter)

	barRegistry := bar.NewRegistry(bar.Options{
		Workspace:             workspace,
		CurrentSessionName:    log.Name(),
		ModelName:             selection.Model,
		ModelEffort:           selection.Effort,
		ModelEffortLevels:     choice.EffortLevels,
		IsFast:                selection.IsFast,
		IsSimulated:           isSimulated,
		UsageReporter:         usageReporter,
		UsageCachePath:        usageCachePath,
		UsageIsSelfRefreshing: usageReporter != nil,
		UsageGauges:           usage.TerminalGauges(keyboard, os.Stdout),
		Currency:              currency,
		SandboxHostname:       hostToSandboxHostname,
		Sources:               app.getBarSources(),
	})
	liveConfig, err := settings.BuildLive(barRegistry)
	if err != nil {
		return "", err
	}
	commandRegistry, err := slash.NewRegistry(systemCommands, liveConfig.SnippetCommandSet)
	if err != nil {
		return "", err
	}
	app.slash.commands = commandRegistry
	app.notifyUnknownSettings(liveConfig.UnknownSettings)
	app.continueMessage = liveConfig.ContinueMessage
	app.display.streamingMode = liveConfig.StreamingMode
	app.display.reasoningRendering = liveConfig.ReasoningRendering
	screen.SetGrouping(liveConfig.Grouping)
	app.display.bar = bar.NewConfiguration(barRegistry, liveConfig.SegmentLayout)

	if resumedSession != nil {
		app.restore(resumedSession)
	}
	for _, failure := range hostToSandboxRestoreResult.Failures {
		correction, err := portgrant.HostToSandboxChangeEvent(
			hostToSandboxAddress,
			failure.Port,
			hostToSandbox.GetCurrent(),
		)
		if err != nil {
			return "", err
		}
		app.pendingNotices.add(correction)
		app.notifyFailure(fmt.Sprintf(
			"Port %d could not be exposed again: %v", failure.Port, failure.Err,
		))
	}
	for _, failure := range sandboxToHostRestoreResult.Failures {
		correction, err := portgrant.SandboxToHostChangeEvent(failure.Port, sandboxToHost.GetCurrent())
		if err != nil {
			return "", err
		}
		app.pendingNotices.add(correction)
		app.notifyFailure(fmt.Sprintf(
			"Host loopback port %d could not be exposed again: %v", failure.Port, failure.Err,
		))
	}
	for _, failure := range pathGrantRestoreResult.Failures {
		correction, err := pathgrant.ChangeEvent(failure.Grant.Path, pathGrants.GetCurrent())
		if err != nil {
			return "", err
		}
		app.queuePathGrantChange(correction)
		app.notifyFailure(fmt.Sprintf(
			"Temporary access to %s could not be restored: %v",
			failure.Grant.Path,
			failure.Err,
		))
	}

	projectSkills, globalSkills := skill.Counts(availableSkills)
	startupElapsed := startup.Elapsed()
	configOverride, hasLocalConfig := settings.GetOverride()
	var localConfig *startup.LocalConfig
	if hasLocalConfig {
		localConfig = &startup.LocalConfig{
			Name:     filepath.Base(configOverride.Path),
			Settings: configOverride.Settings,
		}
	}
	startupInfo := startup.Info{
		Session:       log.Name(),
		PromptBytes:   len(systemPrompt),
		ProjectSkills: projectSkills,
		GlobalSkills:  globalSkills,
		Snippets:      len(settings.Snippets),
		ToolBytes:     client.ToolsSize(enabledTools),
		LocalConfig:   localConfig,
	}
	if resumedSession == nil {
		app.notify(startup.NewEvent(startupElapsed, startupInfo))
	}

	hasStarted = true
	hooks.EmitSessionStarted(ctx, cycle.SessionStarted{Session: sessionInfo})
	transition := app.begin(args.Message)
	*requestedTransition = transition
	stopReason = transition.StopReason()
	hooks.EmitSessionStopping(ctx, cycle.SessionStopping{Session: sessionInfo, Reason: stopReason})

	if isSessionLeftToResume(log.IsPersisted(), isSimulated, transition.Kind) {
		_, _ = fmt.Fprintf(notices, "\n%s\n", style.Subtle(sessions.ResumeCommand(os.Args[0], log.Name())))
	}

	return "", nil
}

func isSessionLeftToResume(isPersisted bool, isSimulated bool, kind cycle.TransitionKind) bool {
	return isPersisted && !isSimulated && kind == cycle.Quit
}

func openRunner(
	ctx context.Context, isYolo bool, hostLoopback []uint16,
) (sandbox.Runner, *jobs.Manager, *keeper.Keeper, func(), error) {
	if isYolo {
		return sandbox.Direct(), nil, nil, func() {}, nil
	}

	keeperProcess, err := keeper.Open(ctx, hostLoopback...)
	if err != nil {
		return sandbox.Direct(), nil, nil, func() {}, err
	}

	runner := sandbox.Wrapped(keeperProcess)

	return runner, jobs.New(runner), keeperProcess, func() { _ = keeperProcess.Close() }, nil
}

func newHostToSandboxExposer(
	ctx context.Context, keeperProcess *keeper.Keeper, host string,
) portgrant.HostToSandboxExposer {
	if keeperProcess == nil {
		return portgrant.HostToSandboxExposer{}
	}

	return portgrant.HostToSandboxExposer{
		Expose: func(port uint16) error { return keeperProcess.OpenHostToSandbox(ctx, host, port) },
		Hide:   keeperProcess.CloseHostToSandbox,
	}
}

func newSandboxToHostExposer(ctx context.Context, keeperProcess *keeper.Keeper) portgrant.SandboxToHostExposer {
	if keeperProcess == nil {
		return portgrant.SandboxToHostExposer{}
	}

	return portgrant.SandboxToHostExposer{
		Expose: func(port uint16) error { return keeperProcess.OpenSandboxToHost(ctx, port) },
		Hide:   keeperProcess.CloseSandboxToHost,
	}
}

func managedJobs(manager *jobs.Manager) commands.Jobs {
	if manager == nil {
		return commands.Jobs{}
	}

	return commands.Jobs{
		List:   manager.List,
		Status: manager.Status,
		Output: manager.Output,
		Stop: func(name string) (agent.Event, error) {
			snapshot, err := manager.Stop(name)
			if err != nil {
				return agent.Event{}, err
			}

			return caps.JobStoppedByUserEvent(snapshot.Name), nil
		},
		Discard:       manager.Discard,
		PruneFinished: manager.PruneFinished,
	}
}
