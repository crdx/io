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
	sessionJournalName    = "session.jsonl"
	sessionTranscriptName = "chat.md"
)

type Options struct {
	ConfigDir  string
	ConfigFile string
	StateDir   string
	Session    Session

	Editor string
	Output io.Writer
}

type Session struct {
	Name      string
	ID        string
	Directory string
}

type commandEnvironment struct {
	configDir  string
	configPath string
	stateDir   string
	session    commandSession

	openEditor func(string) error
	openTarget func(string) error
	copyText   func(string) error
}

type commandSession struct {
	name      string
	id        string
	directory string
}

type commandTarget struct {
	value        string
	confirmation string
	prepare      func() error
}

func New(options Options) slash.CommandSet {
	return newCommands(commandEnvironment{
		configDir:  options.ConfigDir,
		configPath: options.ConfigFile,
		stateDir:   options.StateDir,
		session: commandSession{
			name:      options.Session.Name,
			id:        options.Session.ID,
			directory: options.Session.Directory,
		},
		openEditor: func(path string) error {
			return editor.Open(options.Editor, path)
		},
		openTarget: openDesktopTarget,
		copyText: func(text string) error {
			return terminal.Copy(options.Output, text)
		},
	})
}

func newCommands(environment commandEnvironment) slash.CommandSet {
	var commands slash.CommandSet
	commands = slash.New(
		noArgumentCommand("/conf", "Usage: /conf", environment.configPath, environment.openEditor),
		targetCommand(
			"/copy",
			"Usage: /copy {session-name|session-id|session-dir}",
			map[string]commandTarget{
				"session-name": copyTarget(environment.session.name),
				"session-id":   copyTarget(environment.session.id),
				"session-dir":  copyTarget(environment.session.directory),
			},
			environment.copyText,
		),
		helpCommand(func() string { return helpText(commands.Usages()) }),
		targetCommand(
			"/open",
			"Usage: /open {config-dir|state-dir|session-dir|session-log|session-chat}",
			map[string]commandTarget{
				"config-dir": {
					value: environment.configDir,
					prepare: func() error {
						return os.MkdirAll(environment.configDir, 0o700)
					},
				},
				"state-dir":   {value: environment.stateDir},
				"session-dir": existingTarget("Session directory", environment.session.directory),
				"session-log": existingTarget(
					"Session log",
					filepath.Join(environment.session.directory, sessionJournalName),
				),
				"session-chat": existingTarget(
					"Session chat",
					filepath.Join(environment.session.directory, sessionTranscriptName),
				),
			},
			environment.openTarget,
		),
	)
	return commands
}

func helpCommand(getHelp func() string) slash.Command {
	return slash.Command{
		Name: "/help",
		Run: func(context slash.Context, arguments []string) error {
			if len(arguments) != 0 {
				return slash.Usage("Usage: /help")
			}

			context.Notice(getHelp())
			return nil
		},
	}
}

func helpText(usages []string) string {
	visibleUsages := slices.DeleteFunc(usages, func(usage string) bool { return usage == "/help" })
	return "Commands:\n  " + strings.Join(visibleUsages, "\n  ")
}

func copyTarget(value string) commandTarget {
	return commandTarget{value: value, confirmation: "Copied to clipboard."}
}

func existingTarget(description string, path string) commandTarget {
	return commandTarget{
		value: path,
		prepare: func() error {
			_, err := os.Stat(path)
			switch {
			case errors.Is(err, fs.ErrNotExist):
				return fmt.Errorf("%s does not exist yet", description)
			case err != nil:
				return fmt.Errorf("could not inspect %s: %w", description, err)
			default:
				return nil
			}
		},
	}
}

func noArgumentCommand(name string, usage string, value string, action func(string) error) slash.Command {
	return slash.Command{
		Name: name,
		Run: func(_ slash.Context, arguments []string) error {
			if len(arguments) != 0 {
				return slash.Usage(usage)
			}

			return action(value)
		},
	}
}

func targetCommand(name string, usage string, targets map[string]commandTarget, action func(string) error) slash.Command {
	command := slash.Command{
		Name: name,
		Run: func(context slash.Context, arguments []string) error {
			if len(arguments) != 1 {
				return slash.Usage(usage)
			}

			target, found := targets[arguments[0]]
			if !found {
				return slash.Usage(usage)
			}

			if target.prepare != nil {
				if err := target.prepare(); err != nil {
					return err
				}
			}

			if err := action(target.value); err != nil {
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

func openDesktopTarget(path string) error {
	command := exec.Command("xdg-open", path) //nolint:gosec // the fixed opener receives a path selected by the command
	if err := command.Start(); err != nil {
		return fmt.Errorf("could not open %s: %w", path, err)
	}

	go func() { _ = command.Wait() }()

	return nil
}
