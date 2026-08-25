package commands

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/dispatch"
	"crdx.org/io/cmd/oh/slash"
	"crdx.org/io/cmd/oh/snippets"
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

func newCommandRegistry(t *testing.T, environment commandEnvironment) slash.Registry {
	t.Helper()

	set, err := buildCommands(environment, nil)
	if err != nil {
		t.Fatal(err)
	}
	return commandRegistry(t, set)
}

func newCommandRegistryWithSnippets(
	t *testing.T,
	environment commandEnvironment,
	configuredSnippets map[string]string,
) slash.Registry {
	t.Helper()

	snippetSet, err := snippets.New(configuredSnippets)
	if err != nil {
		t.Fatal(err)
	}
	systemSet, err := buildCommands(environment, snippetSet.Usages())
	if err != nil {
		t.Fatal(err)
	}
	return commandRegistry(t, systemSet, snippetSet)
}

func commandRegistry(t *testing.T, sets ...slash.CommandSet) slash.Registry {
	t.Helper()

	registry, err := slash.NewRegistry(sets...)
	if err != nil {
		t.Fatal(err)
	}
	return registry
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
	commands := newCommandRegistry(t, commandEnvironment{
		configDir:  configDirectory,
		configPath: configPath,
		stateDir:   stateDirectory,
		session: commandSession{
			name:        "brave-otter",
			id:          "session-id",
			directory:   sessionDirectory,
			isPersisted: func() bool { return true },
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
		startSession: func(start SessionStart) error {
			action := "new:" + start.ModelGlob
			if start.SourceSessionName != "" {
				action = "fork:" + start.SourceSessionName + ":" + start.ModelGlob
			}
			actions = append(actions, action)
			return nil
		},
	})

	tests := map[string]string{
		"/conf":              "edit:" + configPath,
		"/new":               "new:",
		"/new sonnet":        "new:sonnet",
		"/fork":              "fork:brave-otter:",
		"/fork sonnet":       "fork:brave-otter:sonnet",
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

func TestForkRequiresAPersistedSession(t *testing.T) {
	isPersisted := false
	startRan := false
	commands := newCommandRegistry(t, commandEnvironment{
		session: commandSession{
			name:        "brave-otter",
			isPersisted: func() bool { return isPersisted },
		},
		startSession: func(SessionStart) error {
			startRan = true
			return nil
		},
	})

	result, failure := dispatch.Handle(commands, dispatch.Actions{}, "/fork")
	if result != dispatch.Rejected {
		t.Fatalf("expected the command to be refused, got result %d", result)
	}
	want := "/fork: Session does not exist yet (alt+enter sends as message)"
	if failure != want {
		t.Errorf("got %q, want %q", failure, want)
	}
	if startRan {
		t.Error("fork started before the session was stored")
	}

	isPersisted = true
	result, failure = dispatch.Handle(commands, dispatch.Actions{}, "/fork")
	if result != dispatch.Handled || failure != "" {
		t.Errorf("stored session got result %d and failure %q", result, failure)
	}
	if !startRan {
		t.Error("fork did not start after the session was stored")
	}
}

func TestCommandsRejectUnknownOrExtraTargets(t *testing.T) {
	commands := newCommandRegistry(t, commandEnvironment{
		session: commandSession{isPersisted: func() bool { return true }},
	})

	for _, input := range []string{
		"/conf extra",
		"/help extra",
		"/new one two",
		"/fork one two",
		"/copy session-name extra",
		"/open unknown",
	} {
		t.Run(input, func(t *testing.T) {
			invocation, found := commands.Find(input)
			if !found {
				t.Fatal("expected command to be found")
			}

			err := invocation.Command.Run(nil, invocation.Arguments)
			if err == nil {
				t.Fatal("expected usage error")
			}
			if !slash.IsUsageError(err) {
				t.Errorf("got error %v", err)
			}
			if message := slash.FormatError(invocation, err); !strings.HasPrefix(message, "Usage: ") {
				t.Errorf("got formatted error %q", message)
			}
		})
	}
}

func TestAModelThatCannotBeResolvedLeavesTheCommandToBeCorrected(t *testing.T) {
	commands := newCommandRegistry(t, commandEnvironment{
		startSession: func(SessionStart) error {
			return errors.New(`model "opus" is ambiguous`)
		},
	})

	result, failure := dispatch.Handle(commands, dispatch.Actions{}, "/new opus")
	if result != dispatch.Rejected {
		t.Fatalf("expected the command to be refused, got result %d", result)
	}

	want := `/new: Model "opus" is ambiguous (alt+enter sends as message)`
	if failure != want {
		t.Errorf("got %q, want %q", failure, want)
	}
}

func TestTargetCommandsExposeTheirArgumentsForCompletion(t *testing.T) {
	commands := newCommandRegistry(t, commandEnvironment{})

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
	commands := newCommandRegistry(t, commandEnvironment{
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

func TestHelpOmitsTheSnippetSectionWhenNoneAreConfigured(t *testing.T) {
	got := helpText([]string{"/conf", "/help"}, "/help", nil)
	if strings.Contains(got, "Snippets:") || strings.Contains(got, "/help") {
		t.Errorf("got help %q", got)
	}
}

func TestBrowseIsNotACommandAnymore(t *testing.T) {
	if _, found := newCommandRegistry(t, commandEnvironment{}).Find("/browse session-dir"); found {
		t.Error("expected /browse to have been folded into /open")
	}
}

func TestConfReportsAnUnconfiguredEditor(t *testing.T) {
	set, err := New(Options{ConfigFile: filepath.Join(t.TempDir(), "config.toml")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	commands := commandRegistry(t, set)
	invocation, found := commands.Find("/conf")
	if !found {
		t.Fatal("expected /conf to be registered")
	}

	err = invocation.Command.Run(nil, invocation.Arguments)
	if err == nil || !strings.Contains(err.Error(), "set editor in config.toml") {
		t.Errorf("got error %v", err)
	}
}
