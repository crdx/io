package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"crdx.org/io/internal/file"
	"crdx.org/io/internal/pathutil"
	"crdx.org/io/internal/sandbox"
	"crdx.org/io/tool"
	"crdx.org/io/toolbox/bash"
)

const (
	shellTimeout   = 5 * time.Minute  // wall clock, which a command spent waiting also spends
	shellCPUTime   = 60 * time.Second // processor time, which only a busy command spends
	shellFileSize  = 1024 << 20       // enough for a build artefact, not enough to fill a disk
	shellOpenFiles = 4096
)

func execPaths(workspace string) []string {
	paths := []string{workspace}

	for entry := range strings.SplitSeq(os.Getenv("PATH"), string(os.PathListSeparator)) {
		if entry == "" {
			entry = workspace
		} else if !filepath.IsAbs(entry) {
			entry = filepath.Join(workspace, entry)
		}

		entry = filepath.Clean(entry)

		info, err := os.Stat(entry)
		if err == nil && info.IsDir() && !slices.Contains(paths, entry) {
			paths = append(paths, entry)
		}
	}

	return paths
}

func shellPolicy(
	ctx context.Context,
	workspace string,
	home string,
	tmpDir string,
	currentCaps caps,
) (sandbox.Policy, error) {
	policy := sandbox.Policy{
		Exec:   append(execPaths(workspace), home, sandbox.TmpDir),
		TmpDir: tmpDir,
		Env:    []string{"PATH", "LANG", "TERM", "USER"},
		SetEnv: map[string]string{
			"GIT_CONFIG_NOSYSTEM": "1",
			"HOME":                home,
			"TMPDIR":              sandbox.TmpDir,
		},
		Timeout:    shellTimeout,
		CPUTime:    shellCPUTime,
		FileSize:   shellFileSize,
		OpenFiles:  shellOpenFiles,
		Background: currentCaps.has(capBackground),
	}

	if modules := goModuleCache(); modules != "" {
		policy.Read = append(policy.Read, modules)
		policy.SetEnv["GOMODCACHE"] = modules
	}

	if grantedPaths := writablePaths(workspace, home, currentCaps); len(grantedPaths) > 0 {
		writable := policy
		writable.Read = slices.Clone(policy.Read)
		writable.Write = grantedPaths

		if !currentCaps.has(capWrite) {
			writable.Read = append(writable.Read, workspace) // read the tree, change only its .git
		}

		if !currentCaps.has(capGit) {
			var err error
			writable, err = protectedPolicy(writable, workspace)
			if err != nil {
				return policy, fmt.Errorf("could not find repository metadata to protect: %w", err)
			}
		}

		writable.Write = append(writable.Write, sandbox.TmpDir)

		if sandbox.Supported(ctx, writable) == nil {
			return writable, nil
		}
	}

	policy.Read = append(policy.Read, workspace, home)
	policy.Write = append(policy.Write, sandbox.TmpDir)

	return policy, sandbox.Supported(ctx, policy)
}

func protectedPolicy(policy sandbox.Policy, workspace string) (sandbox.Policy, error) {
	policy.Read = slices.Clone(policy.Read)

	err := filepath.WalkDir(workspace, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err // missing a branch would grant it write access without knowing what is there
		}
		if entry.Name() != ".git" {
			return nil
		}

		if !slices.Contains(policy.Read, path) {
			policy.Read = append(policy.Read, path)
		}
		if entry.IsDir() {
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return policy, err
	}

	return bash.ProtectedPolicy(policy), nil // also covers .git at other writable roots, such as HOME
}

// ErrShellWithheld is a command turned away before it is confined, the shell not being granted.
var ErrShellWithheld = errors.New(
	"shell access is not granted; the person at the keyboard can grant it with ctrl+x x",
)

func confinedShell( // nothing is prepared: what is granted may change between one command and the next
	workspace string,
	home string,
	tmpDir string,
	mode *Mode,
	files *file.Root,
	processes *sandbox.Processes,
) tool.Tool {
	fresh := func(ctx context.Context) (sandbox.Policy, error) {
		currentCaps := mode.Current()

		if !currentCaps.has(capShell) {
			return sandbox.Policy{}, ErrShellWithheld
		}

		policy, err := shellPolicy(ctx, workspace, home, tmpDir, currentCaps)
		if err != nil {
			return policy, fmt.Errorf("the shell cannot be confined: %w", err)
		}

		return policy, nil
	}

	readOnly := func() bool {
		currentCaps := mode.Current()
		return !currentCaps.has(capShell) || writablePaths(workspace, home, currentCaps) == nil
	}

	return bash.New(files, readOnly, fresh, processes)
}

func writablePaths(workspaceDir string, homeDir string, currentCaps caps) []string {
	switch {
	case currentCaps.has(capWrite):
		return []string{workspaceDir, homeDir}
	case currentCaps.has(capGit):
		if metadata := filepath.Join(workspaceDir, ".git"); pathutil.Exists(metadata) {
			return []string{metadata, homeDir}
		}
	}

	return nil
}
