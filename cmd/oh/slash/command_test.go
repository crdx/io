package slash_test

import (
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

func TestCommandSetCompletesOnlyAUniquePrefix(t *testing.T) {
	commands := slash.New(
		slash.Command{Name: "/conf", Run: commandHandler},
		slash.Command{Name: "/copy", Run: commandHandler},
		slash.Command{Name: "/open", Run: commandHandler},
	)

	for prefix, want := range map[string]string{
		"/op":   "/open",
		"/open": "/open",
	} {
		completion, found := commands.Complete(prefix)
		if !found || completion != want {
			t.Errorf("Complete(%q) got %q and %t, want %q", prefix, completion, found, want)
		}
	}

	for _, prefix := range []string{"", "hello", "/co", "/missing", "/open argument"} {
		if completion, found := commands.Complete(prefix); found {
			t.Errorf("Complete(%q) unexpectedly got %q", prefix, completion)
		}
	}
}
