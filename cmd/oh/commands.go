package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"crdx.org/io/cmd/oh/editor"
	"crdx.org/io/cmd/oh/slash"
	"crdx.org/io/cmd/oh/store"
	"crdx.org/io/cmd/oh/terminal"
)

const (
	sessionJournalName    = "session.jsonl"
	sessionTranscriptName = "chat.md"
)

type commandEnvironment struct {
	configDirectory string
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

func getCommands(log *store.Writer, writer io.Writer, editorName string) slash.CommandSet {
	sessionDirectory := filepath.Join(sessionsDir(), log.Name())

	return newCommands(commandEnvironment{
		configDirectory: configDir(),
		session: commandSession{
			name:      log.Name(),
			id:        log.ID(),
			directory: sessionDirectory,
		},
		openEditor: func(path string) error {
			return editor.Open(editorName, path)
		},
		openTarget: openDesktopTarget,
		copyText: func(text string) error {
			return terminal.Copy(writer, text)
		},
	})
}

func newCommands(environment commandEnvironment) slash.CommandSet {
	return slash.New(
		slash.Command{Name: "/browse", Run: targetCommand(
			"usage: /browse {config-dir|session-dir}",
			map[string]commandTarget{
				"config-dir": {
					value: environment.configDirectory,
					prepare: func() error {
						return os.MkdirAll(environment.configDirectory, 0o700)
					},
				},
				"session-dir": {value: environment.session.directory},
			},
			environment.openTarget,
		)},
		slash.Command{Name: "/conf", Run: noArgumentCommand("usage: /conf", configPath(), environment.openEditor)},
		slash.Command{Name: "/copy", Run: targetCommand(
			"usage: /copy {session-name|session-id|session-dir}",
			map[string]commandTarget{
				"session-name": {value: environment.session.name},
				"session-id":   {value: environment.session.id},
				"session-dir":  {value: environment.session.directory},
			},
			environment.copyText,
		)},
		slash.Command{Name: "/open", Run: targetCommand(
			"usage: /open {session-log|session-chat}",
			map[string]commandTarget{
				"session-log":  {value: filepath.Join(environment.session.directory, sessionJournalName)},
				"session-chat": {value: filepath.Join(environment.session.directory, sessionTranscriptName)},
			},
			environment.openTarget,
		)},
	)
}

func noArgumentCommand(usage string, value string, action func(string) error) func(slash.Context, []string) error {
	return func(_ slash.Context, arguments []string) error {
		if len(arguments) != 0 {
			return errors.New(usage)
		}

		return action(value)
	}
}

func targetCommand(usage string, targets map[string]commandTarget, action func(string) error) func(slash.Context, []string) error {
	return func(_ slash.Context, arguments []string) error {
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
	}
}

func openDesktopTarget(path string) error {
	command := exec.Command("xdg-open", path) //nolint:gosec // the fixed opener receives a path selected by the command
	if err := command.Start(); err != nil {
		return fmt.Errorf("could not open %s: %w", path, err)
	}

	go func() { _ = command.Wait() }()

	return nil
}
