package commands

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/slash"
	"crdx.org/io/cmd/oh/snippets"
)

func TestCompletionMatchesGolden(t *testing.T) {
	commands := newCommandRegistryWithSnippets(t, fixtureEnvironment(t), fixtureSnippets())
	var output strings.Builder

	for _, test := range []struct {
		prefix string
		steps  int
	}{
		{prefix: "/", steps: 6},
		{prefix: "/c", steps: 1},
		{prefix: "/copy ", steps: 3},
		{prefix: "/copy l", steps: 1},
		{prefix: "/copy sn", steps: 1},
		{prefix: "/edit ", steps: 3},
		{prefix: "/edit sn", steps: 1},
		{prefix: "/open ", steps: 12},
		{prefix: "/open sn", steps: 1},
		{prefix: "//", steps: 3},
		{prefix: "//a", steps: 3},
	} {
		state := slash.Completion{}
		current := test.prefix
		fmt.Fprintf(&output, "%s", test.prefix)
		for range test.steps {
			completed, found := state.Next(commands, current)
			if !found {
				t.Fatalf("expected completion for %q", current)
			}
			fmt.Fprintf(&output, " -> %s", completed)
			current = completed
		}
		output.WriteByte('\n')
	}

	got := output.String()
	goldenPath := filepath.Join("testdata", "completion.txt")
	if *updateGoldens {
		if err := os.WriteFile(goldenPath, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	want, err := os.ReadFile(goldenPath) //nolint:gosec // fixed testdata path
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("completion differs from %s\n--- got ---\n%s--- want ---\n%s", goldenPath, got, want)
	}
}

func fixtureEnvironment(t *testing.T) commandEnvironment {
	t.Helper()
	configDirectory := t.TempDir()
	if err := os.Mkdir(filepath.Join(configDirectory, "snippets"), 0o700); err != nil {
		t.Fatal(err)
	}
	return commandEnvironment{configDir: configDirectory}
}

func fixtureSnippets() map[string]snippets.Definition {
	return map[string]snippets.Definition{
		"add": {
			Prompt:      "Add {{.Arg}}",
			Description: "Add a task, then continue.",
			Arguments:   snippets.ArgumentsRequired,
		},
		"ask": {
			Prompt:      "Ask {{.Arg}}",
			Description: "Answer without making changes.",
			Arguments:   snippets.ArgumentsRequired,
		},
		"note": {
			Prompt: "Note this.",
		},
	}
}

var updateGoldens = flag.Bool("update", false, "write command output back to the golden files")

type helpContext struct {
	notice string
}

func (self *helpContext) Emit(agent.Event) {}
func (self *helpContext) Send(string)      {}
func (self *helpContext) Notice(text string) {
	self.notice = text
}
func (self *helpContext) Success(string) {}

func TestHelpMatchesGolden(t *testing.T) {
	commands := newCommandRegistryWithSnippets(t, fixtureEnvironment(t), fixtureSnippets())
	invocation, found := commands.Find("/help")
	if !found {
		t.Fatal("expected /help to be registered")
	}

	context := &helpContext{}
	if err := invocation.Command.Run(context, invocation.Arguments); err != nil {
		t.Fatal(err)
	}
	got := context.notice + "\n"
	goldenPath := filepath.Join("testdata", "help.txt")
	if *updateGoldens {
		if err := os.WriteFile(goldenPath, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	want, err := os.ReadFile(goldenPath) //nolint:gosec // fixed testdata path
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("help differs from %s\n--- got ---\n%s--- want ---\n%s", goldenPath, got, want)
	}
}
