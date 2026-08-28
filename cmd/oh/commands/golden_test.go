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
		{prefix: "/", steps: 7},
		{prefix: "/c", steps: 2},
		{prefix: "/copy ", steps: 3},
		{prefix: "/copy l", steps: 1},
		{prefix: "/copy sn", steps: 1},
		{prefix: "/edit ", steps: 3},
		{prefix: "/edit sn", steps: 1},
		{prefix: "/open ", steps: 12},
		{prefix: "/open sn", steps: 1},
		{prefix: "//", steps: 4},
		{prefix: "//a", steps: 3},
		{prefix: "//h", steps: 1},
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

	assertGolden(t, "completion.txt", output.String())
}

func assertGolden(t *testing.T, name string, got string) {
	t.Helper()

	goldenPath := filepath.Join("testdata", name)
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
		t.Errorf("output differs from %s\n--- got ---\n%s--- want ---\n%s", goldenPath, got, want)
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

func TestSnippetHelpMatchesGolden(t *testing.T) {
	var output strings.Builder
	for _, test := range []struct {
		label              string
		configuredSnippets map[string]snippets.Definition
	}{
		{label: "snippets configured", configuredSnippets: fixtureSnippets()},
		{label: "no snippets configured", configuredSnippets: nil},
	} {
		commands := newCommandRegistryWithSnippets(t, fixtureEnvironment(t), test.configuredSnippets)
		invocation, found := commands.Find("//help")
		if !found {
			t.Fatal("expected //help to be registered")
		}

		context := &helpContext{}
		if err := invocation.Command.Run(context, invocation.Arguments); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&output, "=== %s ===\n%s\n", test.label, context.notice)
	}

	assertGolden(t, "snippet-help.txt", output.String())
}

func expansionSnippets() map[string]snippets.Definition {
	return map[string]snippets.Definition{
		"add":  {Prompt: "Add the following:\n\n{{.Arg}}"},
		"note": {Prompt: "Note this."},
		"tag":  {Prompt: "{{range .Args}}[{{.}}]{{end}}"},
	}
}

func TestSnippetExpansionMatchesGolden(t *testing.T) {
	commands := newCommandRegistryWithSnippets(t, fixtureEnvironment(t), expansionSnippets())
	var output strings.Builder

	for _, input := range []string{
		"//note",
		"//add pay the bill",
		"//add   spaced   out   words  ",
		"//add first line\n\nsecond paragraph\n  - one\n  - two\n",
		"//add\nnothing on the command line\n",
		"//tag one two three",
	} {
		invocation, found := commands.Find(input)
		if !found {
			t.Fatalf("expected %q to be found", input)
		}

		context := &promptContext{}
		if err := invocation.Command.Run(context, invocation.Arguments); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&output, "=== %q ===\n%s\n", input, context.sent)
	}

	assertGolden(t, "snippet-expansion.txt", output.String())
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

type promptContext struct {
	sent string
}

func (self *promptContext) Emit(agent.Event) {}
func (self *promptContext) Send(prompt string) {
	self.sent = prompt
}
func (self *promptContext) Notice(string)  {}
func (self *promptContext) Success(string) {}

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
	assertGolden(t, "help.txt", context.notice+"\n")
}
