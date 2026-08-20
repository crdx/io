package main

import (
	"errors"
	"fmt"
	"os"
	"testing"

	"crdx.org/io/agent"
	"crdx.org/io/session"

	"crdx.org/io/cmd/oh/output"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/ohs/picker"
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
		meta := fmt.Appendf(nil, `{"workspaceDir":"/home/alice/proj/%d"}`, index)
		log, err := session.Create(directory, meta)
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

	sessions, err := loadSessions(directory)
	if err != nil {
		t.Fatal(err)
	}

	chosenSession, err := picker.Choose(sessions, os.Stdin, os.Stdout)
	screen := output.New(os.Stdout)

	switch {
	case errors.Is(err, picker.ErrCancelled):
		screen.Line(style.Cancelled("nothing was chosen"))
	case err != nil:
		t.Fatal(err)
	default:
		screen.Line(style.Result("chose " + chosenSession.ID + ": " + chosenSession.FirstPrompt()))
	}

	screen.End()
}
