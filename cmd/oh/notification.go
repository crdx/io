package main

import (
	"os"
	"os/exec"
	"path/filepath"
)

func sendTurnFinishedNotification(workspaceDir string) {
	command, writesToTerminal := newTurnFinishedNotificationCommand(
		workspaceDir,
		os.Getenv("KITTY_WINDOW_ID") != "",
	)
	if writesToTerminal {
		command.Stdout = os.Stdout
	}
	_ = command.Run()
}

func newTurnFinishedNotificationCommand(workspaceDir string, kitty bool) (*exec.Cmd, bool) {
	title := "oh — " + filepath.Base(workspaceDir)
	body := "A model is waiting to chat"

	if kitty {
		//nolint:gosec // the executable is fixed and the workspace name is passed as one inert argument
		return exec.Command("kitten", "notify", "--icon=utilities-terminal", "--app-name=oh", title, body), true
	}

	//nolint:gosec // the executable is fixed and the workspace name is passed as one inert argument
	return exec.Command("notify-send", "--app-name=oh", "--icon=utilities-terminal", title, body), false
}
