package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
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
	shellTimeout   = 5 * time.Minute
	shellCPUTime   = 60 * time.Second
	shellFileSize  = 1024 << 20
	shellOpenFiles = 4096

	goBuildCacheDir  = "go-build"
	goModuleCacheDir = "go-mod"
	goLangCiLintDir  = "golangci-lint"
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

func furnish(homeDir string, sources []string) ([]string, error) {
	granted := make([]string, 0, len(sources))

	for _, source := range sources {
		relative, below := homeRelativePath(source)
		if !below {
			continue
		}

		target := filepath.Join(homeDir, relative)
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return nil, err
		}

		if err := os.Remove(target); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}

		if err := os.Symlink(source, target); err != nil {
			return nil, err
		}

		granted = append(granted, source)
	}

	return granted, nil
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

	lintCachePath := filepath.Join(".cache", goLangCiLintDir)
	if err := os.MkdirAll(filepath.Join(tmpDir, lintCachePath), 0o700); err != nil {
		return sandbox.Policy{}, fmt.Errorf("could not prepare the shell lint cache: %w", err)
	}

	mappedPaths, err := furnish(homeDir, extraPaths.Home)
	if err != nil {
		return sandbox.Policy{}, fmt.Errorf("could not furnish the shell home: %w", err)
	}

	readablePaths := slices.Concat(extraPaths.Read, extraPaths.Write, mappedPaths)

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
			"GOLANGCI_LINT_CACHE": filepath.Join(sandbox.TmpDir, lintCachePath),
			"GOMODCACHE":          filepath.Join(cacheDir, goModuleCacheDir),
			"HOME":                homeDir,
			"MISE_DATA_DIR":       shellMiseDataDir(),
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

	writablePolicy := grantWriteAccess(policy, writablePathsForPolicy)

	if !currentCaps.has(capWrite) {
		writablePolicy = writablePolicy.WithRead(workspaceDir)
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

func grantWriteAccess(policy sandbox.Policy, paths []string) sandbox.Policy {
	return policy.WithoutRead(paths...).WithWrite(paths...)
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

		resolvedRoot, err := filepath.EvalSymlinks(root)
		if err != nil {
			return policy, err
		}
		if file.InGitDir(resolvedRoot) {
			if !slices.Contains(policy.Read, root) {
				readOnlyPaths = append(readOnlyPaths, root)
			}
			continue
		}

		err = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
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

	return bash.ProtectedPolicy(policy.WithRead(readOnlyPaths...)), nil
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

	return bash.New(files, fresh, processes)
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
