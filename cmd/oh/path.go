package main

import (
	"errors"
	"fmt"
	"path/filepath"

	"crdx.org/io/internal/pathutil"
	"crdx.org/io/internal/sandbox"
	"crdx.org/io/internal/xdgutil"
)

const (
	namespace = "org.crdx"
	app       = "oh"
)

var errWorkspaceShadowed = errors.New("workspace cannot use /tmp because the sandbox shadows it with private scratch space")

func ensureWorkspaceIsNotShadowed(workspaceDir string) error {
	if workspacePathIsShadowed(workspaceDir) {
		return errWorkspaceShadowed
	}

	resolvedWorkspaceDir, err := filepath.EvalSymlinks(workspaceDir)
	if err != nil {
		return fmt.Errorf("could not resolve workspace links: %w", err)
	}
	if workspacePathIsShadowed(resolvedWorkspaceDir) {
		return errWorkspaceShadowed
	}

	return nil
}

func workspacePathIsShadowed(workspaceDir string) bool {
	_, shadowed := pathutil.RelativeTo(sandbox.TmpDir, workspaceDir)
	return shadowed
}

func stateDir(parts ...string) string {
	return xdgutil.StatePath(append([]string{namespace, app}, parts...)...)
}

func configDir(parts ...string) string {
	return xdgutil.ConfigPath(append([]string{namespace, app}, parts...)...)
}

func configPath() string {
	return configDir("config.toml")
}

func globalContextPath() string {
	return configDir("SYSTEM.md")
}

func historyPath() string {
	return stateDir("history")
}

func sessionsDir() string {
	return stateDir("sessions")
}

func shellHomeDir() string {
	return stateDir("home")
}

func tmpDir(id string) string { // one per session, so a resumed one finds what it left
	return stateDir("tmps", id)
}
