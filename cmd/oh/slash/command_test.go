package slash_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"crdx.org/io/cmd/oh/slash"
)

func TestRegistryFindsACommandAndItsArguments(t *testing.T) {
	registry := mustRegistry(t,
		mustSet(t, "/", slash.Command{Name: "open", Run: commandHandler}),
		mustSet(t, "//", slash.Command{Name: "open", Run: commandHandler}),
	)

	invocation, found := registry.Find("  //open session-chat  ")
	if !found {
		t.Fatal("expected //open to be found")
	}
	if invocation.Name != "//open" || invocation.Usage != "//open" || invocation.Command.Name != "open" {
		t.Errorf("got invocation %+v", invocation)
	}
	if !slices.Equal(invocation.Arguments, []string{"session-chat"}) {
		t.Errorf("got arguments %v", invocation.Arguments)
	}
	if _, found := registry.Find("/opening"); found {
		t.Error("expected /opening not to match /open")
	}
}

func TestSetRejectsInvalidDefinitions(t *testing.T) {
	tests := map[string]struct {
		prefix  string
		command slash.Command
	}{
		"empty prefix":    {command: slash.Command{Name: "open", Run: commandHandler}},
		"spaced prefix":   {prefix: "/ ", command: slash.Command{Name: "open", Run: commandHandler}},
		"empty name":      {prefix: "/", command: slash.Command{Run: commandHandler}},
		"prefixed name":   {prefix: "/", command: slash.Command{Name: "/open", Run: commandHandler}},
		"spaced name":     {prefix: "/", command: slash.Command{Name: "open file", Run: commandHandler}},
		"missing handler": {prefix: "/", command: slash.Command{Name: "open"}},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := slash.NewSet(test.prefix, test.command); err == nil {
				t.Error("expected invalid definition to return an error")
			}
		})
	}
}

func TestSetRejectsDuplicateNames(t *testing.T) {
	if _, err := slash.NewSet(
		"/",
		slash.Command{Name: "open", Run: commandHandler},
		slash.Command{Name: "open", Run: commandHandler},
	); err == nil {
		t.Error("expected duplicate command name to return an error")
	}
}

func TestRegistryRejectsDuplicatePrefixes(t *testing.T) {
	first := mustSet(t, "/", slash.Command{Name: "open", Run: commandHandler})
	second := mustSet(t, "/", slash.Command{Name: "copy", Run: commandHandler})
	if _, err := slash.NewRegistry(first, second); err == nil {
		t.Error("expected duplicate prefix to return an error")
	}
}

func commandHandler(slash.Context, []string) error {
	return nil
}

func TestCompletionCyclesThroughNamesWithinTheLongestPrefix(t *testing.T) {
	registry := mustRegistry(t,
		mustSet(t, "/",
			slash.Command{Name: "conf", Run: commandHandler},
			slash.Command{Name: "copy", Run: commandHandler},
			slash.Command{Name: "open", Run: commandHandler},
		),
		mustSet(t, "//",
			slash.Command{Name: "review", Run: commandHandler},
			slash.Command{Name: "rewrite", Run: commandHandler},
		),
	)

	assertCompletionCycle(t, registry, "/co", []string{"/conf", "/copy", "/conf"})
	assertCompletionCycle(t, registry, "//re", []string{"//review", "//rewrite", "//review"})
	assertCompletionCycle(t, registry, "/op", []string{"/open", "/open"})

	for _, prefix := range []string{"", "hello", "/missing", "//missing", "/open argument"} {
		var completion slash.Completion
		if completed, found := completion.Next(registry, prefix); found {
			t.Errorf("Next(%q) unexpectedly got %q", prefix, completed)
		}
	}
}

func TestCompletionCyclesThroughMatchingArguments(t *testing.T) {
	registry := mustRegistry(t, mustSet(t, "/",
		slash.Command{Name: "browse", Run: commandHandler}.WithArguments("config-dir", "session-dir"),
		slash.Command{Name: "conf", Run: commandHandler},
		slash.Command{Name: "copy", Run: commandHandler}.WithArguments("session-name", "session-id", "session-dir"),
		slash.Command{Name: "open", Run: commandHandler}.WithArguments("session-log", "session-chat"),
	))

	assertCompletionCycle(t, registry, "/copy ", []string{
		"/copy session-dir",
		"/copy session-id",
		"/copy session-name",
		"/copy session-dir",
	})
	assertCompletionCycle(t, registry, "/open session-", []string{
		"/open session-chat",
		"/open session-log",
		"/open session-chat",
	})
	assertCompletionCycle(t, registry, "/browse c", []string{"/browse config-dir"})

	for _, prefix := range []string{
		"/conf ",
		"/open session-log extra",
		"/missing anything",
	} {
		var completion slash.Completion
		if completed, found := completion.Next(registry, prefix); found {
			t.Errorf("Next(%q) unexpectedly got %q", prefix, completed)
		}
	}
}

func assertCompletionCycle(t *testing.T, registry slash.Registry, prefix string, wants []string) {
	t.Helper()

	var completion slash.Completion
	current := prefix
	for _, want := range wants {
		completed, found := completion.Next(registry, current)
		if !found || completed != want {
			t.Fatalf("Next(%q) got %q and %t, want %q", current, completed, found, want)
		}
		current = completed
	}
}

func TestCommandNameRecognisesRegisteredPrefixes(t *testing.T) {
	registry := mustRegistry(t,
		mustSet(t, "/"),
		mustSet(t, "//"),
	)

	for input, want := range map[string]string{
		"/unknown":        "/unknown",
		" /unknown arg ":  "/unknown",
		"//unknown":       "//unknown",
		" //unknown arg ": "//unknown",
	} {
		name, found := registry.CommandName(input)
		if !found || name != want {
			t.Errorf("CommandName(%q) got %q and %t", input, name, found)
		}
	}

	for _, input := range []string{"", "hello", "not/a/command"} {
		if name, found := registry.CommandName(input); found {
			t.Errorf("CommandName(%q) unexpectedly got %q", input, name)
		}
	}
}

func TestUsagesComeFromSetMetadata(t *testing.T) {
	set := mustSet(t, "//",
		slash.Command{Name: "review", Run: commandHandler},
		slash.Command{Name: "test", Run: commandHandler}.WithArguments("quick", "all"),
	)
	want := []string{"//review", "//test {all|quick}"}
	if got := set.Usages(); !slices.Equal(got, want) {
		t.Errorf("got usages %v, want %v", got, want)
	}
}

func TestUsageErrorIsRecognised(t *testing.T) {
	err := slash.Usage()
	if !slash.IsUsageError(err) {
		t.Errorf("got %T %q", err, err)
	}
}

func TestCommandErrorsAreFormattedForHarnessMessages(t *testing.T) {
	invocation := slash.Invocation{Name: "/copy", Usage: "/copy {session-dir|session-id|session-name}"}
	if got := slash.FormatError(invocation, slash.Usage()); got != "Usage: /copy {session-dir|session-id|session-name}" {
		t.Errorf("got usage error %q", got)
	}
	invocation = slash.Invocation{Name: "/conf", Usage: "/conf"}
	if got := slash.FormatError(invocation, errors.New("editor is not configured")); got != "/conf: Editor is not configured" {
		t.Errorf("got operational error %q", got)
	}
}

func mustSet(t *testing.T, prefix string, commands ...slash.Command) slash.Set {
	t.Helper()

	set, err := slash.NewSet(prefix, commands...)
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func mustRegistry(t *testing.T, sets ...slash.Set) slash.Registry {
	t.Helper()

	registry, err := slash.NewRegistry(sets...)
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func TestUsageErrorsCanBeWrapped(t *testing.T) {
	if !slash.IsUsageError(errors.Join(errors.New("wrapper"), slash.Usage())) {
		t.Error("expected wrapped usage error to be recognised")
	}
}

func TestOperationalErrorCapitalisesItsFirstRune(t *testing.T) {
	invocation := slash.Invocation{Name: "/open"}
	if got := slash.FormatError(invocation, errors.New("über failure")); !strings.HasPrefix(got, "/open: Über") {
		t.Errorf("got %q", got)
	}
}
