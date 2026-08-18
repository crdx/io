package main

import (
	"errors"
	"fmt"
	"os"
	"testing"

	"crdx.org/io/agent"

	"crdx.org/io/cmd/oh/output"
	"crdx.org/io/cmd/oh/picker"
	"crdx.org/io/cmd/oh/store"
	"crdx.org/io/cmd/oh/theme"
)

var prompts = []string{
	"why does the spinner stutter when a tool runs",
	"add support for reasoning traces",
	"the cancelled turn leaves a tool call unanswered\nand the next request fails",
	"rename the harness to oh",
}

func TestPick(t *testing.T) {
	if os.Getenv("RIG") == "" {
		t.Skip("set RIG to drive the picker")
	}

	directory := t.TempDir()

	for index, prompt := range prompts {
		log, err := store.Create(directory, store.Meta{
			Model:        "gpt-5.6-sol",
			WorkspaceDir: fmt.Sprintf("/home/alice/proj/%d", index),
			Provider:     "codex",
		})
		if err != nil {
			t.Fatal(err)
		}

		if err := log.Event(agent.Event{Kind: agent.Prompt, Text: prompt}); err != nil {
			t.Fatal(err)
		}

		if err := log.Close(); err != nil {
			t.Fatal(err)
		}
	}

	sessions, err := store.List(directory)
	if err != nil {
		t.Fatal(err)
	}

	chosenSession, err := picker.Session(sessions, os.Stdin, os.Stdout)
	screen := output.New(os.Stdout)

	switch {
	case errors.Is(err, picker.ErrCancelled):
		screen.Line(theme.Cancelled("nothing was chosen"))
	case err != nil:
		t.Fatal(err)
	default:
		screen.Line(theme.Result("chose " + chosenSession.ID + ": " + chosenSession.FirstPrompt()))
	}

	screen.End()
}
