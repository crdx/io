package slash_test

import (
	"errors"
	"slices"
	"testing"

	"crdx.org/io/cmd/oh/slash"
)

func TestCommandSetFindsACommandAndItsArguments(t *testing.T) {
	commands := slash.New(slash.Command{
		Name: "/open",
		Run:  func(slash.Context, []string) error { return nil },
	})

	invocation, found := commands.Find("  /open session-chat  ")
	if !found {
		t.Fatal("expected /open to be found")
	}
	if invocation.Command.Name != "/open" {
		t.Errorf("got command %q", invocation.Command.Name)
	}
	if !slices.Equal(invocation.Arguments, []string{"session-chat"}) {
		t.Errorf("got arguments %v", invocation.Arguments)
	}
	if _, found := commands.Find("/opening"); found {
		t.Error("expected /opening not to match /open")
	}
}

func TestCommandSetRejectsInvalidDefinitions(t *testing.T) {
	tests := map[string]slash.Command{
		"empty name":      {Run: commandHandler},
		"bare slash":      {Name: "/", Run: commandHandler},
		"double slash":    {Name: "//open", Run: commandHandler},
		"spaced name":     {Name: "/open file", Run: commandHandler},
		"missing handler": {Name: "/open"},
	}

	for name, command := range tests {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("expected definition to panic")
				}
			}()
			slash.New(command)
		})
	}
}

func commandHandler(slash.Context, []string) error {
	return nil
}

func TestCommandCompletionCyclesThroughMatchingNames(t *testing.T) {
	commands := slash.New(
		slash.Command{Name: "/conf", Run: commandHandler},
		slash.Command{Name: "/copy", Run: commandHandler},
		slash.Command{Name: "/open", Run: commandHandler},
	)

	assertCompletionCycle(t, commands, "/co", []string{"/conf", "/copy", "/conf"})
	assertCompletionCycle(t, commands, "/op", []string{"/open", "/open"})

	for _, prefix := range []string{"", "hello", "/missing", "/open argument"} {
		var completion slash.Completion
		if completed, found := completion.Next(commands, prefix); found {
			t.Errorf("Next(%q) unexpectedly got %q", prefix, completed)
		}
	}
}

func TestCommandCompletionCyclesThroughMatchingArguments(t *testing.T) {
	commands := slash.New(
		slash.Command{Name: "/browse", Run: commandHandler}.WithArguments("config-dir", "session-dir"),
		slash.Command{Name: "/conf", Run: commandHandler},
		slash.Command{Name: "/copy", Run: commandHandler}.WithArguments("session-name", "session-id", "session-dir"),
		slash.Command{Name: "/open", Run: commandHandler}.WithArguments("session-log", "session-chat"),
	)

	assertCompletionCycle(t, commands, "/copy ", []string{
		"/copy session-dir",
		"/copy session-id",
		"/copy session-name",
		"/copy session-dir",
	})
	assertCompletionCycle(t, commands, "/open session-", []string{
		"/open session-chat",
		"/open session-log",
		"/open session-chat",
	})
	assertCompletionCycle(t, commands, "/browse c", []string{"/browse config-dir"})

	for _, prefix := range []string{
		"/conf ",
		"/open session-log extra",
		"/missing anything",
	} {
		var completion slash.Completion
		if completed, found := completion.Next(commands, prefix); found {
			t.Errorf("Next(%q) unexpectedly got %q", prefix, completed)
		}
	}
}

func assertCompletionCycle(t *testing.T, commands slash.CommandSet, prefix string, wants []string) {
	t.Helper()

	var completion slash.Completion
	current := prefix
	for _, want := range wants {
		completed, found := completion.Next(commands, current)
		if !found || completed != want {
			t.Fatalf("Next(%q) got %q and %t, want %q", current, completed, found, want)
		}
		current = completed
	}
}

func TestCommandNameRecognisesSlashInput(t *testing.T) {
	for input, want := range map[string]string{
		"/unknown":       "/unknown",
		" /unknown arg ": "/unknown",
	} {
		name, found := slash.CommandName(input)
		if !found || name != want {
			t.Errorf("CommandName(%q) got %q and %t", input, name, found)
		}
	}

	for _, input := range []string{"", "hello", "not/a/command"} {
		if name, found := slash.CommandName(input); found {
			t.Errorf("CommandName(%q) unexpectedly got %q", input, name)
		}
	}
}

func TestUsageErrorIsRecognised(t *testing.T) {
	err := slash.Usage("Usage: /copy")
	if !slash.IsUsageError(err) || err.Error() != "Usage: /copy" {
		t.Errorf("got %T %q", err, err)
	}
}

func TestCommandErrorsAreFormattedForHarnessMessages(t *testing.T) {
	if got := slash.FormatError("/copy", slash.Usage("Usage: /copy")); got != "Usage: /copy" {
		t.Errorf("got usage error %q", got)
	}
	if got := slash.FormatError("/conf", errors.New("editor is not configured")); got != "/conf: Editor is not configured" {
		t.Errorf("got operational error %q", got)
	}
}
