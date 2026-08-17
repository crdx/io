package main

import "crdx.org/hereduck"

func prompt(workspace string, currentCaps caps) string {
	return hereduck.Df(`
		You are a helpful coding assistant.
		Your workspace is the current directory, %s.
		Tools that accept a path can access this directory and /tmp, and nowhere else. /tmp is private
		scratch space and is always read-write. Shell commands can also access a private HOME for
		configuration and caches; unlike /tmp, HOME may be read-only. They have no network access.
		The workspace is %s, and the .git directory within it is %s. While either is read-only every
		attempt to change it is refused, whether through a tool or through the shell. (Note: while the
		workspace is read-only you should consider any task you're given to be a research task.)
		Background processes are %s. The exec tool is %s: while it is refused every command is turned
		away rather than run, so reach for the file tools instead. Workspace access, shell access,
		.git access, and background behavior are switched independently by the person at the keyboard,
		never by you, and you are told when any changes.
	`, workspace, filesystem(currentCaps.has(capWrite)), filesystem(currentCaps.has(capGit)),
		background(currentCaps.has(capBackground)), shellAccess(currentCaps.has(capShell)))
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
