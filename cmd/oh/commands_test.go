package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestCommandsRunWithoutStoppingTheHarness(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	configDirectory := filepath.Join(configHome, namespace, app)
	sessionDirectory := filepath.Join(t.TempDir(), "sessions", "brave-otter")
	var actions []string
	commands := newCommands(commandEnvironment{
		configDirectory: configDirectory,
		session: commandSession{
			name:      "brave-otter",
			id:        "session-id",
			directory: sessionDirectory,
		},
		openEditor: func(path string) error {
			actions = append(actions, "edit:"+path)
			return nil
		},
		openTarget: func(path string) error {
			actions = append(actions, "open:"+path)
			return nil
		},
		copyText: func(text string) error {
			actions = append(actions, "copy:"+text)
			return nil
		},
	})

	tests := map[string]string{
		"/conf":               "edit:" + configPath(),
		"/browse config-dir":  "open:" + configDirectory,
		"/browse session-dir": "open:" + sessionDirectory,
		"/copy session-name":  "copy:brave-otter",
		"/copy session-id":    "copy:session-id",
		"/copy session-dir":   "copy:" + sessionDirectory,
		"/open session-log":   "open:" + filepath.Join(sessionDirectory, sessionJournalName),
		"/open session-chat":  "open:" + filepath.Join(sessionDirectory, sessionTranscriptName),
	}

	for input, wantAction := range tests {
		t.Run(input, func(t *testing.T) {
			actions = nil
			invocation, found := commands.Find(input)
			if !found {
				t.Fatal("expected command to be found")
			}
			if err := invocation.Command.Run(commandContext{}, invocation.Arguments); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(actions, []string{wantAction}) {
				t.Errorf("got actions %v, want %q", actions, wantAction)
			}
		})
	}

	if _, err := os.Stat(configDirectory); err != nil {
		t.Errorf("config directory was not prepared: %v", err)
	}
}

func TestCommandsRejectUnknownOrExtraTargets(t *testing.T) {
	commands := newCommands(commandEnvironment{})

	for _, input := range []string{
		"/conf extra",
		"/browse",
		"/browse elsewhere",
		"/copy session-name extra",
		"/open unknown",
	} {
		t.Run(input, func(t *testing.T) {
			invocation, found := commands.Find(input)
			if !found {
				t.Fatal("expected command to be found")
			}

			err := invocation.Command.Run(commandContext{}, invocation.Arguments)
			if err == nil || !strings.Contains(err.Error(), "usage: ") {
				t.Errorf("got error %v", err)
			}
		})
	}
}
