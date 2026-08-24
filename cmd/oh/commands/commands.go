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

	"crdx.org/io/cmd/oh/editor"
	"crdx.org/io/cmd/oh/slash"
	"crdx.org/io/cmd/oh/terminal"
)

const (
	sessionJournalName    = "session.jsonl"
	sessionTranscriptName = "chat.md"
)

type Options struct {
	ConfigDirectory string
	ConfigPath      string
	Editor          string
	Output          io.Writer
	Session         Session
}

type Session struct {
	Name      string
	ID        string
	Directory string
}

type commandEnvironment struct {
	configDirectory string
	configPath      string
	session         commandSession
	openEditor      func(string) error
	openTarget      func(string) error
	copyText        func(string) error
}

type commandSession struct {
	name      string
	id        string
	directory string
}

type commandTarget struct {
	value   string
	prepare func() error
}

func New(options Options) slash.CommandSet {
	return newCommands(commandEnvironment{
		configDirectory: options.ConfigDirectory,
		configPath:      options.ConfigPath,
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
	return slash.New(
		targetCommand(
			"/browse",
			"usage: /browse {config-dir|session-dir}",
			map[string]commandTarget{
				"config-dir": {
					value: environment.configDirectory,
					prepare: func() error {
						return os.MkdirAll(environment.configDirectory, 0o700)
					},
				},
				"session-dir": existingTarget("session directory", environment.session.directory),
			},
			environment.openTarget,
		),
		noArgumentCommand("/conf", "usage: /conf", environment.configPath, environment.openEditor),
		targetCommand(
			"/copy",
			"usage: /copy {session-name|session-id|session-dir}",
			map[string]commandTarget{
				"session-name": {value: environment.session.name},
				"session-id":   {value: environment.session.id},
				"session-dir":  {value: environment.session.directory},
			},
			environment.copyText,
		),
		targetCommand(
			"/open",
			"usage: /open {session-log|session-chat}",
			map[string]commandTarget{
				"session-log": existingTarget(
					"session log",
					filepath.Join(environment.session.directory, sessionJournalName),
				),
				"session-chat": existingTarget(
					"session chat",
					filepath.Join(environment.session.directory, sessionTranscriptName),
				),
			},
			environment.openTarget,
		),
	)
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
				return errors.New(usage)
			}

			return action(value)
		},
	}
}

func targetCommand(name string, usage string, targets map[string]commandTarget, action func(string) error) slash.Command {
	command := slash.Command{
		Name: name,
		Run: func(_ slash.Context, arguments []string) error {
			if len(arguments) != 1 {
				return errors.New(usage)
			}

			target, found := targets[arguments[0]]
			if !found {
				return errors.New(usage)
			}

			if target.prepare != nil {
				if err := target.prepare(); err != nil {
					return err
				}
			}

			return action(target.value)
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
