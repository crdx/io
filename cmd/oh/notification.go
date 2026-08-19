package main

import (
	"os/exec"
	"path/filepath"
)

func sendTurnFinishedNotification(workspaceDir string) {
	body := "A model in " + filepath.Base(workspaceDir) + " is waiting for you"

	//nolint:gosec // the executable is fixed and the workspace name is passed as one inert argument
	_ = exec.Command("notify-send", "--app-name=oh", "oh", body).Run()
}
