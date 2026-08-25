package commands

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"crdx.org/io/cmd/oh/editor"
	"crdx.org/io/cmd/oh/slash"
	"crdx.org/io/cmd/oh/terminal"
)

const (
	systemCommandPrefix   = "/"
	sessionJournalName    = "session.jsonl"
	sessionTranscriptName = "chat.md"
)

type Options struct {
	ConfigDir        string
	ConfigFile       string
	SystemPromptFile string
	SkillDirs        []string
	WorkspaceDir     string
	ScratchDir       string
	HomeDir          string
	Session          Session

	Editor       editor.Command
	Output       io.Writer
	StartSession func(SessionStart) error
}

type Session struct {
	Name        string
	ID          string
	Directory   string
	IsPersisted func() bool
}

type SessionStart struct {
	ModelGlob         string
	SourceSessionName string
}

type commandEnvironment struct {
	configDir        string
	configPath       string
	systemPromptPath string
	skillDirs        []string
	workspaceDir     string
	scratchDir       string
	homeDir          string
	session          commandSession

	openEditor   func([]string) error
	openTarget   func([]string) error
	copyText     func([]string) error
	startSession func(SessionStart) error
}

type commandSession struct {
	name        string
	id          string
	directory   string
	isPersisted func() bool
}

type commandTarget struct {
	resolveValues func() ([]string, error)
	confirmation  string
}

func New(options Options, snippetUsages []string) (slash.CommandSet, error) {
	return buildCommands(commandEnvironment{
		configDir:        options.ConfigDir,
		configPath:       options.ConfigFile,
		systemPromptPath: options.SystemPromptFile,
		skillDirs:        options.SkillDirs,
		workspaceDir:     options.WorkspaceDir,
		scratchDir:       options.ScratchDir,
		homeDir:          options.HomeDir,
		session: commandSession{
			name:        options.Session.Name,
			id:          options.Session.ID,
			directory:   options.Session.Directory,
			isPersisted: options.Session.IsPersisted,
		},
		openEditor: func(paths []string) error {
			return editor.Open(options.Editor, paths...)
		},
		openTarget: openDesktopTargets,
		copyText: func(values []string) error {
			return terminal.Copy(options.Output, strings.Join(values, "\n"))
		},
		startSession: options.StartSession,
	}, snippetUsages)
}

func buildCommands(environment commandEnvironment, snippetUsages []string) (slash.CommandSet, error) {
	var set slash.CommandSet
	var help slash.Command
	help = helpCommand(func() string {
		return helpText(set.Usages(), systemCommandPrefix+help.Name, snippetUsages)
	})
	targets := locationTargets(environment)
	commands := []slash.Command{
		targetCommand(
			"copy",
			map[string]commandTarget{
				"session-name": copyTarget(environment.session.name),
				"session-id":   copyTarget(environment.session.id),
				"session-dir":  copyTarget(environment.session.directory),
			},
			environment.copyText,
		),
		targetCommand("edit", targets, environment.openEditor),
		help,
		sessionCommand("new", func(modelGlob string) error {
			return environment.startSession(SessionStart{ModelGlob: modelGlob})
		}),
		targetCommand("open", targets, environment.openTarget),
	}
	commands = append(commands, commandsRequiringPersistedSession(
		environment.session.isPersisted,
		sessionCommand("fork", func(modelGlob string) error {
			return environment.startSession(SessionStart{
				ModelGlob:         modelGlob,
				SourceSessionName: environment.session.name,
			})
		}),
	)...)

	var err error
	set, err = slash.NewCommandSet(systemCommandPrefix, commands...)
	return set, err
}

func locationTargets(environment commandEnvironment) map[string]commandTarget {
	prepareConfigDir := func() error {
		return os.MkdirAll(environment.configDir, 0o700)
	}

	return map[string]commandTarget{
		"config-dir":    preparedTarget(prepareConfigDir, environment.configDir),
		"config-file":   preparedTarget(prepareConfigDir, environment.configPath),
		"system-prompt": preparedTarget(prepareConfigDir, environment.systemPromptPath),
		"skills-dir":    existingTarget("Skills directory", environment.skillDirs...),
		"workspace-dir": staticTarget(environment.workspaceDir),
		"scratch-dir":   staticTarget(environment.scratchDir),
		"home-dir":      staticTarget(environment.homeDir),
		"session-dir":   existingTarget("Session directory", environment.session.directory),
		"session-log": existingTarget(
			"Session log",
			filepath.Join(environment.session.directory, sessionJournalName),
		),
		"session-chat": existingTarget(
			"Session chat",
			filepath.Join(environment.session.directory, sessionTranscriptName),
		),
	}
}

func helpCommand(getHelp func() string) slash.Command {
	return slash.Command{
		Name: "help",
		Run: func(context slash.Context, arguments []string) error {
			if len(arguments) != 0 {
				return slash.Usage()
			}

			context.Notice(getHelp())
			return nil
		},
	}
}

func sessionCommand(name string, startSession func(string) error) slash.Command {
	return slash.Command{
		Name: name,
		Run: func(_ slash.Context, arguments []string) error {
			if len(arguments) > 1 {
				return slash.Usage()
			}
			if len(arguments) == 0 {
				return startSession("")
			}
			return startSession(arguments[0])
		},
	}
}

func commandsRequiringPersistedSession(isSessionPersisted func() bool, commands ...slash.Command) []slash.Command {
	for i := range commands {
		run := commands[i].Run
		commands[i].Run = func(context slash.Context, arguments []string) error {
			if !isSessionPersisted() {
				return errors.New("session does not exist yet")
			}
			return run(context, arguments)
		}
	}
	return commands
}

func helpText(commandUsages []string, hiddenCommandUsage string, snippetUsages []string) string {
	visibleCommandUsages := slices.DeleteFunc(commandUsages, func(usage string) bool { return usage == hiddenCommandUsage })
	for i, usage := range visibleCommandUsages {
		if usage == "/new" || usage == "/fork" {
			visibleCommandUsages[i] += " [model[@effort]]"
		}
	}
	sections := []string{"Commands:\n  " + strings.Join(visibleCommandUsages, "\n  ")}
	if len(snippetUsages) > 0 {
		sections = append(sections, "Snippets:\n  "+strings.Join(snippetUsages, "\n  "))
	}
	return strings.Join(sections, "\n\n")
}

func copyTarget(value string) commandTarget {
	target := staticTarget(value)
	target.confirmation = "Copied to clipboard."
	return target
}

func staticTarget(values ...string) commandTarget {
	return commandTarget{
		resolveValues: func() ([]string, error) { return values, nil },
	}
}

func preparedTarget(prepare func() error, values ...string) commandTarget {
	return commandTarget{
		resolveValues: func() ([]string, error) {
			if err := prepare(); err != nil {
				return nil, err
			}
			return values, nil
		},
	}
}

func existingTarget(description string, paths ...string) commandTarget {
	return commandTarget{
		resolveValues: func() ([]string, error) {
			var present []string
			for _, path := range paths {
				switch _, err := os.Stat(path); {
				case errors.Is(err, fs.ErrNotExist):
					continue
				case err != nil:
					return nil, fmt.Errorf("could not inspect %s: %w", description, err)
				default:
					present = append(present, path)
				}
			}
			if len(present) == 0 {
				return nil, fmt.Errorf("%s does not exist yet", description)
			}
			return present, nil
		},
	}
}

func targetCommand(name string, targets map[string]commandTarget, action func([]string) error) slash.Command {
	command := slash.Command{
		Name: name,
		Run: func(context slash.Context, arguments []string) error {
			if len(arguments) != 1 {
				return slash.Usage()
			}

			target, found := targets[arguments[0]]
			if !found {
				return slash.Usage()
			}

			values, err := target.resolveValues()
			if err != nil {
				return err
			}

			if err := action(values); err != nil {
				return err
			}
			if target.confirmation != "" {
				context.Success(target.confirmation)
			}
			return nil
		},
	}

	return command.WithArguments(slices.Sorted(maps.Keys(targets))...)
}

func openDesktopTargets(paths []string) error {
	for _, path := range paths {
		command := exec.Command("xdg-open", path) //nolint:gosec // the fixed opener receives a path selected by the command
		if err := command.Start(); err != nil {
			return fmt.Errorf("could not open %s: %w", path, err)
		}

		go func() { _ = command.Wait() }()
	}

	return nil
}
