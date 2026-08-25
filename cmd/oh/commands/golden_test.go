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
)

func TestCompletionMatchesGolden(t *testing.T) {
	commands := newCommandRegistryWithSnippets(t, commandEnvironment{}, fixtureSnippets())
	var output strings.Builder

	for _, test := range []struct {
		prefix string
		steps  int
	}{
		{prefix: "/", steps: 5},
		{prefix: "/c", steps: 2},
		{prefix: "/copy ", steps: 3},
		{prefix: "/open ", steps: 5},
		{prefix: "//", steps: 2},
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

func fixtureSnippets() map[string]string {
	return map[string]string{
		"add": "Add {{.Arg}}",
		"ask": "Ask {{.Arg}}",
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
	commands := newCommandRegistryWithSnippets(t, commandEnvironment{}, fixtureSnippets())
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
