package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"crdx.org/io/internal/sandbox"
	"crdx.org/io/internal/util/pathutil"
	"crdx.org/io/internal/xdg"
	"crdx.org/io/session"
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
	return xdg.StatePath(append([]string{namespace, app}, parts...)...)
}

func configDir(parts ...string) string {
	return xdg.ConfigPath(append([]string{namespace, app}, parts...)...)
}

func configPath() string {
	return configDir("config.toml")
}

func globalContextPath() string {
	return configDir("SYSTEM.md")
}

func modelCachePath() string {
	if os.Getenv(endpointVariable) != "" {
		return stateDir("models.sim.json")
	}

	return stateDir("models.json")
}

func modelRoundRobinPath() string {
	return stateDir("model-round-robin.json")
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

func tmpDir(name string) string {
	return stateDir("tmps", name)
}

func refuseOutdatedSessions(directory string) error {
	outdated, err := session.Outdated(directory)
	if err != nil {
		return err
	}

	if len(outdated) == 0 {
		return nil
	}

	subject := fmt.Sprintf("%d stored sessions are", len(outdated))
	object := "them"

	if len(outdated) == 1 {
		subject = outdated[0] + " is"
		object = "it"
	}

	return fmt.Errorf(
		"%s written in an older journal format: run `ohctl migrate` to bring %s up to format %d",
		subject, object, session.Format,
	)
}
