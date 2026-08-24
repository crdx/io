package commands

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/slash"
)

type commandTestContext struct {
	success string
}

func (self *commandTestContext) Emit(agent.Event) {}
func (self *commandTestContext) Send(string)      {}
func (self *commandTestContext) Notice(string)    {}
func (self *commandTestContext) Success(text string) {
	self.success = text
}

func TestCommandsRunWithoutStoppingTheHarness(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	configDirectory := filepath.Join(configHome, "org.crdx", "oh")
	configPath := filepath.Join(configDirectory, "config.toml")
	stateDirectory := filepath.Join(t.TempDir(), "state")
	sessionDirectory := filepath.Join(t.TempDir(), "sessions", "brave-otter")
	if err := os.MkdirAll(sessionDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{sessionJournalName, sessionTranscriptName} {
		if err := os.WriteFile(filepath.Join(sessionDirectory, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var actions []string
	commands := newCommands(commandEnvironment{
		configDir:  configDirectory,
		configPath: configPath,
		stateDir:   stateDirectory,
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
		"/conf":              "edit:" + configPath,
		"/open config-dir":   "open:" + configDirectory,
		"/open state-dir":    "open:" + stateDirectory,
		"/open session-dir":  "open:" + sessionDirectory,
		"/copy session-name": "copy:brave-otter",
		"/copy session-id":   "copy:session-id",
		"/copy session-dir":  "copy:" + sessionDirectory,
		"/open session-log":  "open:" + filepath.Join(sessionDirectory, sessionJournalName),
		"/open session-chat": "open:" + filepath.Join(sessionDirectory, sessionTranscriptName),
	}

	for input, wantAction := range tests {
		t.Run(input, func(t *testing.T) {
			actions = nil
			invocation, found := commands.Find(input)
			if !found {
				t.Fatal("expected command to be found")
			}
			context := &commandTestContext{}
			if err := invocation.Command.Run(context, invocation.Arguments); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(actions, []string{wantAction}) {
				t.Errorf("got actions %v, want %q", actions, wantAction)
			}
			wantSuccess := ""
			if strings.HasPrefix(input, "/copy ") {
				wantSuccess = "Copied to clipboard."
			}
			if context.success != wantSuccess {
				t.Errorf("got success %q, want %q", context.success, wantSuccess)
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
		"/help extra",
		"/copy session-name extra",
		"/open unknown",
	} {
		t.Run(input, func(t *testing.T) {
			invocation, found := commands.Find(input)
			if !found {
				t.Fatal("expected command to be found")
			}

			err := invocation.Command.Run(nil, invocation.Arguments)
			if err == nil || !strings.Contains(err.Error(), "Usage: ") {
				t.Errorf("got error %v", err)
			}
		})
	}
}

func TestTargetCommandsExposeTheirArgumentsForCompletion(t *testing.T) {
	commands := newCommands(commandEnvironment{})

	for prefix, want := range map[string]string{
		"/open c":         "/open config-dir",
		"/copy session-n": "/copy session-name",
		"/open session-l": "/open session-log",
	} {
		var state slash.Completion
		completion, found := state.Next(commands, prefix)
		if !found || completion != want {
			t.Errorf("Complete(%q) got %q and %t, want %q", prefix, completion, found, want)
		}
	}
}

func TestCommandsReportSessionTargetsThatDoNotExistYet(t *testing.T) {
	sessionDirectory := filepath.Join(t.TempDir(), "missing-session")
	actionRan := false
	commands := newCommands(commandEnvironment{
		session: commandSession{directory: sessionDirectory},
		openTarget: func(string) error {
			actionRan = true
			return nil
		},
	})

	for input, want := range map[string]string{
		"/open session-dir":  "Session directory does not exist yet",
		"/open session-log":  "Session log does not exist yet",
		"/open session-chat": "Session chat does not exist yet",
	} {
		t.Run(input, func(t *testing.T) {
			actionRan = false
			invocation, found := commands.Find(input)
			if !found {
				t.Fatal("expected command to be found")
			}

			err := invocation.Command.Run(nil, invocation.Arguments)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("got error %v, want %q", err, want)
			}
			if actionRan {
				t.Error("action ran for a missing session target")
			}
		})
	}
}

func TestBrowseIsNotACommandAnymore(t *testing.T) {
	if _, found := newCommands(commandEnvironment{}).Find("/browse session-dir"); found {
		t.Error("expected /browse to have been folded into /open")
	}
}

func TestConfReportsAnUnconfiguredEditor(t *testing.T) {
	commands := New(Options{ConfigFile: filepath.Join(t.TempDir(), "config.toml")})
	invocation, found := commands.Find("/conf")
	if !found {
		t.Fatal("expected /conf to be registered")
	}

	err := invocation.Command.Run(nil, invocation.Arguments)
	if err == nil || !strings.Contains(err.Error(), "set editor in config.toml") {
		t.Errorf("got error %v", err)
	}
}
