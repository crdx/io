package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
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

	goBuildCacheDir  = "go-build"
	goModuleCacheDir = "go-mod"
)

func execPaths(workspaceDir string) []string {
	paths := []string{workspaceDir}

	for entry := range strings.SplitSeq(os.Getenv("PATH"), string(os.PathListSeparator)) {
		if entry == "" {
			entry = workspaceDir
		} else if !filepath.IsAbs(entry) {
			entry = filepath.Join(workspaceDir, entry)
		}

		entry = filepath.Clean(entry)

		info, err := os.Stat(entry)
		if err == nil && info.IsDir() && !slices.Contains(paths, entry) {
			paths = append(paths, entry)
		}
	}

	return paths
}

func createSandboxPolicy(
	ctx context.Context,
	workspaceDir string,
	homeDir string,
	tmpDir string,
	extraPaths configuredPaths,
	currentCaps caps,
) (sandbox.Policy, error) {
	cacheDir := filepath.Join(homeDir, ".cache")
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return sandbox.Policy{}, fmt.Errorf("could not prepare the shell cache: %w", err)
	}

	readablePaths := slices.Concat(extraPaths.Read, extraPaths.Write)
	executablePaths := append(append(execPaths(workspaceDir), extraPaths.Exec...), homeDir, sandbox.TmpDir)

	policy := sandbox.Policy{
		Read:  readablePaths,
		Write: []string{cacheDir},
		Exec:  executablePaths,

		TmpDir: tmpDir,
		Env: []string{
			"PATH",
			"LANG",
			"TERM",
			"USER",
		},
		SetEnv: map[string]string{
			"GIT_CONFIG_NOSYSTEM": "1",
			"GOCACHE":             filepath.Join(cacheDir, goBuildCacheDir),
			"GOMODCACHE":          filepath.Join(cacheDir, goModuleCacheDir),
			"HOME":                homeDir,
			"TMPDIR":              sandbox.TmpDir,
		},

		Timeout:   shellTimeout,
		CPUTime:   shellCPUTime,
		FileSize:  shellFileSize,
		OpenFiles: shellOpenFiles,

		Background: currentCaps.has(capBackground),
	}

	policy = policy.WithSetEnv("GOPROXY", "off").WithSetEnv("GOSUMDB", "off")
	modules, err := goModuleCache()
	if err != nil {
		return policy, err
	}
	if modules != "" {
		proxyDir := filepath.Join(modules, "cache", "download")
		proxyURL := (&url.URL{Scheme: "file", Path: proxyDir}).String()
		policy = policy.WithRead(proxyDir).WithSetEnv("GOPROXY", proxyURL)
	}

	writablePathsForPolicy := allWritablePaths(workspaceDir, homeDir, extraPaths.Write, currentCaps)
	if len(writablePathsForPolicy) == 0 {
		return readOnlySandboxPolicy(ctx, policy, workspaceDir, homeDir)
	}

	writablePolicy := policy
	if currentCaps.has(capWrite) {
		writablePolicy = writablePolicy.WithoutRead(extraPaths.Write...)
	}
	writablePolicy = writablePolicy.WithWrite(writablePathsForPolicy...)

	if !currentCaps.has(capWrite) {
		writablePolicy = writablePolicy.WithRead(workspaceDir) // read the tree, change only its .git
	}

	if !currentCaps.has(capGit) {
		protectRoots := append([]string{workspaceDir}, extraPaths.Write...)
		var err error
		writablePolicy, err = protectedPolicy(writablePolicy, protectRoots)
		if err != nil {
			return policy, fmt.Errorf("could not find repository metadata to protect: %w", err)
		}
	}

	writablePolicy = writablePolicy.WithWrite(sandbox.TmpDir)
	if sandbox.Supported(ctx, writablePolicy) != nil {
		return readOnlySandboxPolicy(ctx, policy, workspaceDir, homeDir)
	}

	return writablePolicy, nil
}

func readOnlySandboxPolicy(
	ctx context.Context,
	policy sandbox.Policy,
	workspaceDir string,
	home string,
) (sandbox.Policy, error) {
	policy = policy.WithRead(workspaceDir, home).WithWrite(sandbox.TmpDir)

	return policy, sandbox.Supported(ctx, policy)
}

func protectedPolicy(policy sandbox.Policy, roots []string) (sandbox.Policy, error) {
	var readOnlyPaths []string
	visited := make(map[string]struct{}, len(roots))

	for _, root := range roots {
		root = filepath.Clean(root)
		if _, seen := visited[root]; seen {
			continue
		}
		visited[root] = struct{}{}

		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err // missing a branch would grant it write access without knowing what is there
			}
			if entry.Name() != ".git" {
				return nil
			}

			if !slices.Contains(policy.Read, path) && !slices.Contains(readOnlyPaths, path) {
				readOnlyPaths = append(readOnlyPaths, path)
			}
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		})
		if err != nil {
			return policy, err
		}
	}

	return bash.ProtectedPolicy(policy.WithRead(readOnlyPaths...)), nil // also covers .git at other writable roots, such as HOME
}

// ErrShellWithheld is a command turned away before it is confined, the shell not being granted.
var ErrShellWithheld = errors.New(
	"shell access is not granted; the user can grant it with ctrl+x x",
)

func confinedShell(
	workspaceDir string,
	home string,
	tmpDir string,
	extraPaths configuredPaths,
	mode *Mode,
	files *file.Root,
	processes *sandbox.Processes,
) tool.Tool {
	fresh := func(ctx context.Context) (sandbox.Policy, error) {
		currentCaps := mode.Current()

		if !currentCaps.has(capShell) {
			return sandbox.Policy{}, ErrShellWithheld
		}

		policy, err := createSandboxPolicy(ctx, workspaceDir, home, tmpDir, extraPaths, currentCaps)
		if err != nil {
			if ctx.Err() != nil {
				return policy, ctx.Err()
			}
			return policy, fmt.Errorf("the shell cannot be confined: %w", err)
		}

		return policy, nil
	}

	readOnly := func() bool {
		currentCaps := mode.Current()
		return !currentCaps.has(capShell) || allWritablePaths(workspaceDir, home, extraPaths.Write, currentCaps) == nil
	}

	return bash.New(files, readOnly, fresh, processes)
}

func allWritablePaths(workspaceDir, homeDir string, extraPaths []string, currentCaps caps) []string {
	paths := writablePaths(workspaceDir, homeDir, currentCaps)

	switch {
	case currentCaps.has(capWrite):
		paths = append(paths, extraPaths...)
	case currentCaps.has(capGit):
		for _, directory := range extraPaths {
			if metadata := filepath.Join(directory, ".git"); pathutil.Exists(metadata) {
				paths = append(paths, metadata)
			}
		}
	}

	return paths
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
