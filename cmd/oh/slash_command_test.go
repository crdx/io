package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/edit"
	"crdx.org/io/cmd/oh/key"
	"crdx.org/io/cmd/oh/output"
	"crdx.org/io/cmd/oh/slash"
	"crdx.org/io/cmd/oh/snippets"
	"crdx.org/io/cmd/oh/store"
	"crdx.org/io/cmd/oh/turn"
)

func TestSlashCommandRunsImmediately(t *testing.T) {
	ran := false
	fixtureCommands := fixtureCommandRegistry(t, slash.Command{
		Name: "fixture",
		Run: func(_ slash.Context, arguments []string) error {
			if !slices.Equal(arguments, []string{"one", "two"}) {
				t.Errorf("got arguments %v", arguments)
			}
			ran = true
			return nil
		},
	})

	self := slashCommandFixture(t, caps.Read|caps.Shell)
	self.commands = fixtureCommands
	if got := self.handleSlashCommand("/fixture one two"); got != handledCommand {
		t.Fatalf("got slash input result %d", got)
	}
	if !ran {
		t.Error("expected the command to run immediately")
	}
}

func TestSlashCommandCanAddANotice(t *testing.T) {
	fixtureCommands := fixtureCommandRegistry(t, slash.Command{
		Name: "fixture",
		Run: func(context slash.Context, _ []string) error {
			context.Notice("fixture notice")
			return nil
		},
	})

	self := slashCommandFixture(t, caps.Read)
	self.screen = output.New(&bytes.Buffer{})
	self.commands = fixtureCommands
	if got := self.handleSlashCommand("/fixture"); got != handledCommand {
		t.Fatalf("got slash input result %d", got)
	}
	if len(self.events) != 1 {
		t.Fatalf("got events %v", self.events)
	}
	if self.events[0].Kind != agent.HarnessMessageEvent || self.events[0].Text != "fixture notice" {
		t.Errorf("got event %v", self.events[0])
	}
}

func TestUnknownSlashCommandShowsAnErrorAndKeepsTheInput(t *testing.T) {
	self := slashCommandFixture(t, caps.Read)
	self.screen = output.New(&bytes.Buffer{})
	self.commands = fixtureCommandRegistry(t)
	editor := edit.NewInput(nil)
	for _, value := range "/unknown" {
		editor.Apply(key.Key{Code: key.Rune, Value: value}, false)
	}

	self.acceptInput(editor, nil)

	if got := editor.Text(); got != "/unknown" {
		t.Errorf("got input %q", got)
	}
	if len(self.events) != 1 {
		t.Fatalf("got events %v", self.events)
	}
	want := "Command not found: /unknown; press alt+enter to send anyway"
	if self.events[0].Text != want || self.events[0].Status != agent.ErrorStatus {
		t.Errorf("got event %+v, want failed %q", self.events[0], want)
	}
}

func TestSnippetKeepsItsInvocationInHistoryAndQueuesItsRenderedPrompt(t *testing.T) {
	self := slashCommandFixture(t, caps.Read)
	self.commands = fixtureSnippetRegistry(t, map[string]string{
		"add": "Add the following:\n\n{{ .Arg }}",
	})
	self.currentTurn = Turn{Stream: testTurnStream(nil, func() {}, turn.State{Running: true})}

	historyPath := filepath.Join(t.TempDir(), "history")
	history := edit.NewHistory(historyPath, historyLimit)
	editor := edit.NewInput(history)
	for _, value := range "//add review this" {
		editor.Apply(key.Key{Code: key.Rune, Value: value}, true)
	}
	self.acceptInput(editor, history)

	pending := self.queuedTurn.Peek()
	if !pending.Replacement || pending.Message != "Add the following:\n\nreview this" {
		t.Errorf("got queued turn %+v", pending)
	}
	body, err := os.ReadFile(historyPath) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "//add review this\n" {
		t.Errorf("got history %q", body)
	}
	if editor.Text() != "" {
		t.Errorf("got editor text %q", editor.Text())
	}
}

func TestSnippetWithoutArgumentsShowsUsageAndKeepsTheInput(t *testing.T) {
	self := slashCommandFixture(t, caps.Read)
	self.screen = output.New(&bytes.Buffer{})
	self.commands = fixtureSnippetRegistry(t, map[string]string{
		"add": "Add the following:\n\n{{ .Arg }}",
	})
	history := edit.NewHistory("", historyLimit)
	editor := edit.NewInput(history)
	for _, value := range "//add" {
		editor.Apply(key.Key{Code: key.Rune, Value: value}, false)
	}

	self.acceptInput(editor, history)

	if editor.Text() != "//add" {
		t.Errorf("got editor text %q", editor.Text())
	}
	if len(self.events) != 1 || self.events[0].Text != "Usage: //add <args>; press alt+enter to send anyway" {
		t.Errorf("got events %+v", self.events)
	}
}

func TestPlainSnippetInputWaitsForTheRenderedPrompt(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	self.commands = fixtureSnippetRegistry(t, map[string]string{
		"ask": "Question: {{index .Args 0}} / {{.Arg}}",
	})

	historyPath := filepath.Join(t.TempDir(), "history")
	history := edit.NewHistory(historyPath, historyLimit)
	self.acceptPlainInput(history, "//ask why now")

	var userMessages []string
	for _, event := range self.events {
		if event.Kind == agent.UserMessageEvent {
			userMessages = append(userMessages, event.Text)
		}
	}
	if !slices.Equal(userMessages, []string{"Question: why / why now"}) {
		t.Errorf("got user messages %q", userMessages)
	}
	body, err := os.ReadFile(historyPath) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "//ask why now\n" {
		t.Errorf("got history %q", body)
	}
}

func TestSnippetTemplateErrorsAreReportedByTheHarness(t *testing.T) {
	self := slashCommandFixture(t, caps.Read)
	self.screen = output.New(&bytes.Buffer{})
	self.commands = fixtureSnippetRegistry(t, map[string]string{
		"review": "{{index .Args 2}}",
	})

	if got := self.handleSlashCommand("//review only-one"); got != handledCommand {
		t.Fatalf("got slash input result %d", got)
	}
	if len(self.events) != 1 || !strings.HasPrefix(self.events[0].Text, "//review: Could not render template:") {
		t.Errorf("got events %+v", self.events)
	}
}

func TestUnknownSnippetShowsAnErrorAndKeepsTheInput(t *testing.T) {
	self := slashCommandFixture(t, caps.Read)
	self.screen = output.New(&bytes.Buffer{})
	self.commands = fixtureSnippetRegistry(t, nil)
	editor := edit.NewInput(nil)
	for _, value := range "//unknown" {
		editor.Apply(key.Key{Code: key.Rune, Value: value}, false)
	}

	self.acceptInput(editor, nil)

	if got := editor.Text(); got != "//unknown" {
		t.Errorf("got input %q", got)
	}
	want := "Command not found: //unknown; press alt+enter to send anyway"
	if len(self.events) != 1 || self.events[0].Text != want || self.events[0].Status != agent.ErrorStatus {
		t.Errorf("got events %+v, want failed %q", self.events, want)
	}
}

func TestTabCompletionKeepsCommandNamespacesSeparate(t *testing.T) {
	systemSet, err := slash.NewCommandSet(
		"/",
		slash.Command{Name: "conf", Run: slashTestHandler},
		slash.Command{Name: "copy", Run: slashTestHandler},
	)
	if err != nil {
		t.Fatal(err)
	}
	snippetSet, err := snippets.New(map[string]string{
		"test":   "Run tests.",
		"review": "Review changes.",
	})
	if err != nil {
		t.Fatal(err)
	}
	self := slashCommandFixture(t, caps.Read)
	self.commands = fixtureRegistry(t, systemSet, snippetSet)

	for input, want := range map[string]string{
		"/":  "/conf",
		"//": "//review",
	} {
		self.completion.Reset()
		editor := edit.NewInput(nil)
		for _, value := range input {
			editor.Apply(key.Key{Code: key.Rune, Value: value}, false)
		}
		self.apply(editor, nil, key.Key{Code: key.Rune, Value: '\t'})
		if got := editor.Text(); got != want {
			t.Errorf("completion for %q got %q, want %q", input, got, want)
		}
	}
}

func TestTabCompletesAUniqueSlashCommand(t *testing.T) {
	self := slashCommandFixture(t, caps.Read)
	self.commands = fixtureCommandRegistry(t,
		slash.Command{Name: "conf", Run: slashTestHandler},
		slash.Command{Name: "copy", Run: slashTestHandler},
		slash.Command{Name: "open", Run: slashTestHandler},
	)
	editor := edit.NewInput(nil)
	for _, value := range "/op" {
		editor.Apply(key.Key{Code: key.Rune, Value: value}, false)
	}

	self.apply(editor, nil, key.Key{Code: key.Rune, Value: '\t'})

	if got := editor.Text(); got != "/open" {
		t.Errorf("got completion %q", got)
	}
}

func slashTestHandler(slash.Context, []string) error {
	return nil
}

func fixtureCommandRegistry(t *testing.T, commands ...slash.Command) slash.Registry {
	t.Helper()

	set, err := slash.NewCommandSet("/", commands...)
	if err != nil {
		t.Fatal(err)
	}
	return fixtureRegistry(t, set)
}

func fixtureSnippetRegistry(t *testing.T, configured map[string]string) slash.Registry {
	t.Helper()

	systemSet, err := slash.NewCommandSet("/")
	if err != nil {
		t.Fatal(err)
	}
	snippetSet, err := snippets.New(configured)
	if err != nil {
		t.Fatal(err)
	}
	return fixtureRegistry(t, systemSet, snippetSet)
}

func fixtureRegistry(t *testing.T, sets ...slash.CommandSet) slash.Registry {
	t.Helper()

	registry, err := slash.NewRegistry(sets...)
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func slashCommandFixture(t *testing.T, currentCaps caps.Set) *Harness {
	t.Helper()

	log, err := store.Create(t.TempDir(), store.Meta{Model: "gpt"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })

	return &Harness{
		recorder: recordSession(log),
		mode:     caps.NewMode(currentCaps),
	}
}

func TestConsecutiveTabsCycleCommandArguments(t *testing.T) {
	self := slashCommandFixture(t, caps.Read)
	self.commands = fixtureCommandRegistry(t,
		slash.Command{Name: "copy", Run: slashTestHandler}.WithArguments("session-name", "session-id", "session-dir"),
	)
	editor := edit.NewInput(nil)
	for _, value := range "/copy " {
		editor.Apply(key.Key{Code: key.Rune, Value: value}, false)
	}

	self.apply(editor, nil, key.Key{Code: key.Rune, Value: '\t'})
	if got := editor.Text(); got != "/copy session-dir" {
		t.Errorf("got first completion %q", got)
	}

	self.apply(editor, nil, key.Key{Code: key.Rune, Value: '\t'})
	if got := editor.Text(); got != "/copy session-id" {
		t.Errorf("got second completion %q", got)
	}
}

func TestSlashCommandCanAddASuccessMessage(t *testing.T) {
	fixtureCommands := fixtureCommandRegistry(t, slash.Command{
		Name: "fixture",
		Run: func(context slash.Context, _ []string) error {
			context.Success("Copied to clipboard.")
			return nil
		},
	})

	self := slashCommandFixture(t, caps.Read)
	self.screen = output.New(&bytes.Buffer{})
	self.commands = fixtureCommands
	if got := self.handleSlashCommand("/fixture"); got != handledCommand {
		t.Fatalf("got slash input result %d", got)
	}
	if len(self.events) != 1 || self.events[0].Status != agent.SuccessStatus {
		t.Errorf("got events %+v", self.events)
	}
}

func TestUsageErrorIsNotPrefixedWithTheCommandName(t *testing.T) {
	self := slashCommandFixture(t, caps.Read)
	self.screen = output.New(&bytes.Buffer{})
	self.commands = fixtureCommandRegistry(t, slash.Command{
		Name: "copy",
		Run: func(slash.Context, []string) error {
			return slash.Usage()
		},
	}.WithArguments("session-name", "session-id", "session-dir"))

	if got := self.handleSlashCommand("/copy"); got != rejectedCommand {
		t.Fatalf("got slash input result %d", got)
	}
	want := "Usage: /copy {session-dir|session-id|session-name}; press alt+enter to send anyway"
	if len(self.events) != 1 || self.events[0].Text != want {
		t.Errorf("got events %+v, want %q", self.events, want)
	}
}
