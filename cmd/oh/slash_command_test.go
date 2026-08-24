package main

import (
	"bytes"
	"slices"
	"testing"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/edit"
	"crdx.org/io/cmd/oh/key"
	"crdx.org/io/cmd/oh/output"
	"crdx.org/io/cmd/oh/slash"
	"crdx.org/io/cmd/oh/store"
)

func TestSlashCommandRunsImmediately(t *testing.T) {
	ran := false
	fixtureCommands := slash.New(slash.Command{
		Name: "/fixture",
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
	fixtureCommands := slash.New(slash.Command{
		Name: "/fixture",
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
	self.commands = slash.New()
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
	want := "command not found: /unknown; press alt+enter to send anyway"
	if self.events[0].Text != want || !self.events[0].Failed {
		t.Errorf("got event %+v, want failed %q", self.events[0], want)
	}
}

func TestTabCompletesAUniqueSlashCommand(t *testing.T) {
	self := slashCommandFixture(t, caps.Read)
	self.commands = slash.New(
		slash.Command{Name: "/conf", Run: slashTestHandler},
		slash.Command{Name: "/copy", Run: slashTestHandler},
		slash.Command{Name: "/open", Run: slashTestHandler},
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
	self.commands = slash.New(
		slash.Command{Name: "/copy", Run: slashTestHandler}.WithArguments("session-name", "session-id", "session-dir"),
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
