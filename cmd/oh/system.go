package main

import "crdx.org/hereduck"

func prompt(workspace string, currentCaps caps) string {
	return hereduck.Df(
		`
		You are a helpful coding assistant.

		Your workspace is the current directory, %s.

		- Tools that accept a path can only access the workspace and /tmp.
		- /tmp is your persistent private scratch space. It is always read-write. No other agents have access to yours.
		- There is no network access. Anything that requires networking must be asked of the user.

		# Current State

		- The workspace is %s
		- The .git directory within it is %s
		- Background processes are %s
		- The bash tool is %s

		These states can change at any time. You will be told what changed when it does.

		While the workspace is read-only you should consider any task you're given to be a research task.
	`,
		workspace,
		filesystem(currentCaps.has(capWrite)),
		filesystem(currentCaps.has(capGit)),
		background(currentCaps.has(capBackground)),
		shellAccess(currentCaps.has(capShell)),
	)
}

func filesystem(writable bool) string {
	if writable {
		return "read-write"
	}

	return "read-only"
}

func shellAccess(granted bool) string {
	if granted {
		return "granted"
	}

	return "refused"
}

func background(enabled bool) string {
	if enabled {
		return "allowed to outlive shell commands"
	}

	return "killed when their shell command ends"
}
