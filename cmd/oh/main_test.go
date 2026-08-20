package main

import (
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"crdx.org/duckopt/v2"
	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/store"
	"crdx.org/io/internal/file"
	"crdx.org/io/internal/sandbox"
)

func TestMain(testingMain *testing.M) {
	sandbox.Init()
	os.Exit(testingMain.Run())
}

func bind(t *testing.T, arguments ...string) InputOpts {
	t.Helper()

	originalArgs := os.Args
	t.Cleanup(func() { os.Args = originalArgs })

	os.Args = append([]string{"oh"}, arguments...)

	bound, err := duckopt.Bind[InputOpts](usage, "$0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return *bound
}

func parseOptions(t *testing.T, arguments ...string) Opts {
	t.Helper()

	settledOptions, err := bind(t, arguments...).parse()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return settledOptions
}

var everyCap = []caps{
	0,
	capWrite,
	capGit,
	capWrite | capGit,
	capBackground,
	capWrite | capBackground,
	capGit | capBackground,
	capWrite | capGit | capBackground,
}

func TestEveryOptionIsRead(t *testing.T) {
	useCachedModels(t)

	parsedOptions := parseOptions(t, "-c", "r", "-d", "somewhere", "-m", "deepseek@hi")

	if parsedOptions.caps != capRead {
		t.Errorf("expected reading alone, got %s", parsedOptions.caps.Flags())
	}

	if parsedOptions.workspaceDir != "somewhere" {
		t.Errorf("expected the directory, got %q", parsedOptions.workspaceDir)
	}

	if parsedOptions.provider != opencodeGoProvider || parsedOptions.model != "deepseek-v4-pro" || parsedOptions.effort != "high" {
		t.Errorf("expected opencode-go/deepseek-v4-pro@high, got %s/%s@%s", parsedOptions.provider, parsedOptions.model, parsedOptions.effort)
	}

	id := "0347juX1xcrL9W0QKJe0cs"

	if parsedOptions := parseOptions(t, "-r", id); parsedOptions.session != id || !parsedOptions.resuming() {
		t.Errorf("expected the session, got %q", parsedOptions.session)
	}
}

func TestModelSelectionRequiresModelAndEffort(t *testing.T) {
	for _, selection := range []string{"model", "model@", "@high", "model@high@extra"} {
		if _, err := (InputOpts{Model: selection}).parse(); err == nil {
			t.Errorf("expected %q to be rejected", selection)
		}
	}
}

func TestASessionMayBeResumedWithAPromptBesideIt(t *testing.T) {
	parsedOptions := parseOptions(t, "-r", "0347juX1xcrL9W0QKJe0cs", "carry", "on")

	if !parsedOptions.resuming() {
		t.Error("expected the session to be resumed")
	}

	if parsedOptions.initialMessage != "carry on" {
		t.Errorf("expected the prompt beside it, got %q", parsedOptions.initialMessage)
	}
}

func TestTheVersionIsAskedForOnItsOwn(t *testing.T) {
	for _, argument := range []string{"--version", "-V"} {
		if !bind(t, argument).Version {
			t.Errorf("expected the version to be asked for by %s", argument)
		}
	}

	if version() == "" {
		t.Error("expected a version, got nothing")
	}
}

func TestWhateverIsLeftOverIsTheFirstThingSaid(t *testing.T) {
	parsedOptions := parseOptions(t, "why", "does", "the", "spinner", "stutter")

	if parsedOptions.initialMessage != "why does the spinner stutter" {
		t.Errorf("expected the words back as one, got %q", parsedOptions.initialMessage)
	}

	if parsedOptions.workspaceDir != "." {
		t.Errorf("expected the current directory, got %q", parsedOptions.workspaceDir)
	}
}

func TestTheWorkingDirectoryIsNotTakenFromThePrompt(t *testing.T) {
	parsedOptions := parseOptions(t, "read", "main.go", "-d", "/tmp")

	if parsedOptions.workspaceDir != "/tmp" {
		t.Errorf("expected the directory to come from the option, got %q", parsedOptions.workspaceDir)
	}

	if parsedOptions.initialMessage != "read main.go" {
		t.Errorf("expected the rest to be the prompt, got %q", parsedOptions.initialMessage)
	}
}

func TestTheDefaultCapabilitiesAreEverythingButTheHistory(t *testing.T) {
	parsedOptions := parseOptions(t)

	if got := parsedOptions.caps.Flags(); got != "rxw" {
		t.Errorf("expected rxw, got %q", got)
	}

	if parsedOptions.initialMessage != "" {
		t.Errorf("expected nothing said, got %q", parsedOptions.initialMessage)
	}
}

func TestCapabilitiesAreReadAsTheLettersTheyAreSpelledWith(t *testing.T) {
	for _, capString := range []string{"rwxgb", "bgxwr", "wxgb"} {
		currentCaps, err := Caps(capString)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", capString, err)
		}

		if got := currentCaps.Flags(); got != capFlags {
			t.Errorf("%s: expected it written back as %s, got %q", capString, capFlags, got)
		}
	}

	if _, err := Caps("rwz"); err == nil {
		t.Error("expected a letter naming no capability to be refused")
	}
}

func TestReadingIsAlwaysGranted(t *testing.T) {
	grantedCaps, err := Caps("")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if got := grantedCaps.Flags(); got != "r" {
		t.Errorf("expected r, got %q", got)
	}
}

func TestPromptSeparatesTheWorkspaceFromTmp(t *testing.T) {
	system := harnessContext("/workspace", "session-id", "/state/tmps/session", capRead, configuredPaths{})

	if want := "The workspace (/workspace) is " + filesystem(false); !strings.Contains(system, want) {
		t.Errorf("expected the workspace to be reported as %q, got %q", want, system)
	}

	if want := "The .git directory within it (/workspace/.git) is " + filesystem(false); !strings.Contains(system, want) {
		t.Errorf("expected the history to be reported as %q, got %q", want, system)
	}

	if !strings.Contains(system, "always "+filesystem(true)) {
		t.Errorf("expected the scratch to be writable whatever the workspace is, got %q", system)
	}

	if !strings.Contains(system, "/tmp maps to /state/tmps/session on the user's machine") {
		t.Errorf("expected the scratch backing directory to be reported, got %q", system)
	}

	if !strings.Contains(system, "/tmp/result.png → /state/tmps/session/result.png") {
		t.Errorf("expected an example translated scratch path, got %q", system)
	}

	if strings.Contains(system, "including /tmp") {
		t.Errorf("the workspace mode still claims to include /tmp: %q", system)
	}
}

func TestPromptStatesWhetherTheShellCanRun(t *testing.T) {
	for name, test := range map[string]struct {
		currentCaps caps
		granted     bool
	}{
		"granted": {capRead | capShell, true},
		"refused": {capRead, false},
	} {
		t.Run(name, func(t *testing.T) {
			got := harnessContext("/workspace", "session-id", "/state/tmps/session", test.currentCaps, configuredPaths{})

			if want := "The bash tool is " + shellAccess(test.granted); !strings.Contains(got, want) {
				t.Errorf("expected %q in %q", want, got)
			}

			if unwanted := "The bash tool is " + shellAccess(!test.granted); strings.Contains(got, unwanted) {
				t.Errorf("expected no %q in %q", unwanted, got)
			}
		})
	}
}

func TestAWithheldShellIsStillOfferedAndTurnsCommandsAway(t *testing.T) {
	workspaceRoot, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workspaceRoot.Close() }()

	files := file.New(workspaceRoot, func(string) error { return file.ErrReadOnly })
	mode := NewMode(capRead)
	processes := sandbox.NewProcesses(false)
	defer func() { _, _ = processes.Disable() }()

	shell := confinedShell(t.TempDir(), t.TempDir(), t.TempDir(), configuredPaths{}, mode, files, processes)

	if shell.Name() != "bash" {
		t.Errorf("expected the shell to be offered as bash, got %q", shell.Name())
	}

	if !shell.ReadOnly() {
		t.Error("expected a shell that cannot run to be read-only")
	}

	call, err := shell.Parse(`{"command":"echo one"}`)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := call.Exec(t.Context()); !errors.Is(err, ErrShellWithheld) {
		t.Errorf("expected the command to be turned away, got %v", err)
	}
}

func TestCommandsKeepTheMiseDataDirectoryAfterACapabilityChange(t *testing.T) {
	workspace := t.TempDir()
	workspaceRoot, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workspaceRoot.Close() }()

	miseDataDir := t.TempDir()
	t.Setenv("MISE_DATA_DIR", miseDataDir)

	home := t.TempDir()
	tmp := t.TempDir()
	if _, err := createSandboxPolicy(t.Context(), workspace, home, tmp, configuredPaths{}, capRead|capShell); err != nil {
		t.Skipf("the sandbox cannot enforce a shell policy here: %v", err)
	}

	files := file.New(workspaceRoot, func(string) error { return file.ErrReadOnly })
	mode := NewMode(capRead | capShell)
	processes := sandbox.NewProcesses(false)
	defer func() { _, _ = processes.Disable() }()

	shell := confinedShell(workspace, home, tmp, configuredPaths{}, mode, files, processes)
	run := func() {
		call, parseErr := shell.Parse(`{"command":"printf %s \"$MISE_DATA_DIR\""}`)
		if parseErr != nil {
			t.Fatal(parseErr)
		}

		result, execErr := call.Exec(t.Context())
		if execErr != nil {
			t.Fatal(execErr)
		}
		if result.Output != miseDataDir {
			t.Errorf("got %q, want %q", result.Output, miseDataDir)
		}
	}

	run()
	mode.Toggle(capWrite)
	run()
}

func TestCommandsMayWriteRepositoryMetadataAfterGitIsGranted(t *testing.T) {
	workspace := t.TempDir()
	metadata := filepath.Join(workspace, ".git")
	if err := os.Mkdir(metadata, 0o700); err != nil {
		t.Fatal(err)
	}

	workspaceRoot, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workspaceRoot.Close() }()

	home := t.TempDir()
	tmp := t.TempDir()
	initialCaps := capRead | capWrite | capShell
	if _, err := createSandboxPolicy(t.Context(), workspace, home, tmp, configuredPaths{}, initialCaps); err != nil {
		t.Skipf("the sandbox cannot enforce a shell policy here: %v", err)
	}

	mode := NewMode(initialCaps)
	files := file.New(workspaceRoot, refuseWrite(mode))
	processes := sandbox.NewProcesses(false)
	defer func() { _, _ = processes.Disable() }()
	shell := confinedShell(workspace, home, tmp, configuredPaths{}, mode, files, processes)

	run := func() error {
		call, parseErr := shell.Parse(`{"command":"touch .git/proof"}`)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		_, execErr := call.Exec(t.Context())
		return execErr
	}

	if err := run(); err == nil {
		t.Fatal("expected repository metadata to remain read-only before git was granted")
	}

	mode.Toggle(capGit)
	if err := run(); err != nil {
		t.Fatalf("repository metadata remained read-only after git was granted: %v", err)
	}
}

func TestTmpMountIsWritableWithoutAShell(t *testing.T) {
	workspaceRoot, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workspaceRoot.Close() }()

	files := file.New(workspaceRoot, func(string) error { return file.ErrReadOnly })
	tmp := t.TempDir()
	tmpRoot, err := mountTmpDir(files, tmp)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tmpRoot.Close() }()

	resolvedRoot, name, err := files.Resolve("/tmp/proof")
	if err != nil {
		t.Fatal(err)
	}
	if err := resolvedRoot.WriteFile(name, []byte("written"), 0o600); err != nil {
		t.Fatalf("tmp was not writable: %v", err)
	}
}

func TestGrantingWriteAccessRemovesExactReadOnlyMounts(t *testing.T) {
	workspace := "/workspace"
	home := "/home"
	readOnlyFile := "/workspace/reference"
	policy := grantWriteAccess(sandbox.Policy{
		Read: []string{workspace, home, readOnlyFile},
	}, []string{workspace, home})

	for _, writableRoot := range []string{workspace, home} {
		if slices.Contains(policy.Read, writableRoot) {
			t.Errorf("writable root %s is also mounted read-only: %#v", writableRoot, policy)
		}
		if !slices.Contains(policy.Write, writableRoot) {
			t.Errorf("writable root %s was not granted write access: %#v", writableRoot, policy)
		}
	}
	if !slices.Contains(policy.Read, readOnlyFile) {
		t.Errorf("nested read-only file lost its protection: %#v", policy)
	}
}

func TestTheShellMayUseItsWorkspaceAndHome(t *testing.T) {
	workspace := t.TempDir()
	home := t.TempDir()
	miseDataDir := t.TempDir()
	t.Setenv("MISE_DATA_DIR", miseDataDir)

	policy, err := createSandboxPolicy(t.Context(), workspace, home, t.TempDir(), configuredPaths{}, capWrite)
	if err != nil {
		t.Skipf("the sandbox cannot enforce a writable policy here: %v", err)
	}

	if !policy.Writable() {
		t.Skip("the sandbox fell back to a read-only policy here")
	}

	for _, path := range []string{workspace, home} {
		if !slices.Contains(policy.Write, path) {
			t.Errorf("expected %s to be writable, got %v", path, policy.Write)
		}
	}

	for _, path := range []string{workspace, home} {
		if !slices.Contains(policy.Exec, path) {
			t.Errorf("expected artefacts in %s to be executable, got %v", path, policy.Exec)
		}
	}

	if policy.SetEnv["HOME"] != home {
		t.Errorf("got HOME %q, want %q", policy.SetEnv["HOME"], home)
	}

	if policy.SetEnv["MISE_DATA_DIR"] != miseDataDir {
		t.Errorf("got MISE_DATA_DIR %q, want %q", policy.SetEnv["MISE_DATA_DIR"], miseDataDir)
	}

	if policy.SetEnv["TMPDIR"] != sandbox.TmpDir {
		t.Errorf("got TMPDIR %q, want %q", policy.SetEnv["TMPDIR"], sandbox.TmpDir)
	}
}

func TestBackgroundModeReachesTheShellPolicy(t *testing.T) {
	policy, err := createSandboxPolicy(t.Context(), t.TempDir(), t.TempDir(), t.TempDir(), configuredPaths{}, capBackground)
	if err != nil {
		t.Skipf("the sandbox cannot enforce background mode here: %v", err)
	}
	if !policy.Background {
		t.Error("background mode did not reach the shell policy")
	}
}

func TestTmpIsAlwaysWritable(t *testing.T) {
	tmp := t.TempDir()
	workspace := t.TempDir()
	home := t.TempDir()

	readOnly, err := createSandboxPolicy(t.Context(), workspace, home, tmp, configuredPaths{}, 0)
	if err != nil {
		t.Skipf("the sandbox cannot enforce a read-only policy here: %v", err)
	}
	if !slices.Contains(readOnly.Write, sandbox.TmpDir) {
		t.Errorf("expected writable %s, got %v", sandbox.TmpDir, readOnly.Write)
	}

	readWrite, err := createSandboxPolicy(t.Context(), workspace, home, tmp, configuredPaths{}, capWrite)
	if err != nil {
		t.Skipf("the sandbox cannot enforce a writable policy here: %v", err)
	}
	if !readWrite.Writable() {
		t.Skip("the sandbox fell back to a read-only policy here")
	}
	if !slices.Contains(readWrite.Write, sandbox.TmpDir) {
		t.Errorf("expected writable %s, got %v", sandbox.TmpDir, readWrite.Write)
	}

	if readWrite.TmpDir != tmp {
		t.Errorf("got tmp dir %q, want %q", readWrite.TmpDir, tmp)
	}
	if !slices.Contains(readWrite.Exec, sandbox.TmpDir) {
		t.Errorf("expected artefacts in %s to be executable, got %v", sandbox.TmpDir, readWrite.Exec)
	}
}

func TestConfiguredPathsReachTheShellPolicy(t *testing.T) {
	workspace := t.TempDir()
	home := t.TempDir()
	readDirectory := t.TempDir()
	writeDirectory := t.TempDir()
	execDirectory := t.TempDir()
	additional := configuredPaths{
		Read: []string{readDirectory}, Write: []string{writeDirectory}, Exec: []string{execDirectory},
	}

	readOnly, err := createSandboxPolicy(t.Context(), workspace, home, t.TempDir(), additional, 0)
	if err != nil {
		t.Skipf("the sandbox cannot enforce the configured policy here: %v", err)
	}
	for _, path := range []string{readDirectory, writeDirectory} {
		if !slices.Contains(readOnly.Read, path) {
			t.Errorf("expected %s to be readable, got %v", path, readOnly.Read)
		}
	}
	if !slices.Contains(readOnly.Exec, execDirectory) {
		t.Errorf("expected %s to be executable, got %v", execDirectory, readOnly.Exec)
	}
	if slices.Contains(readOnly.Write, writeDirectory) {
		t.Errorf("configured write path is writable without write capability: %v", readOnly.Write)
	}

	readWrite, err := createSandboxPolicy(t.Context(), workspace, home, t.TempDir(), additional, capWrite)
	if err != nil {
		t.Skipf("the sandbox cannot enforce the configured writable policy here: %v", err)
	}
	if !readWrite.Writable() {
		t.Skip("the sandbox fell back to a read-only policy here")
	}
	if !slices.Contains(readWrite.Write, writeDirectory) {
		t.Errorf("expected %s to be writable, got %v", writeDirectory, readWrite.Write)
	}
	if slices.Contains(readWrite.Read, writeDirectory) {
		t.Errorf("configured write path remained read-only inside its write grant: %v", readWrite.Read)
	}
}

func TestCommandsOnThePathMayBeExecuted(t *testing.T) {
	workspace := t.TempDir()
	installDir := t.TempDir()
	t.Setenv("PATH", installDir)

	if paths := execPaths(workspace); !slices.Contains(paths, installDir) {
		t.Errorf("got %v, want it to include %s", paths, installDir)
	}
}

func TestGoUsesTheShellCacheAndTheHostModuleCacheAsAProxy(t *testing.T) {
	workspace := t.TempDir()
	home := t.TempDir()
	hostModules := filepath.Join(t.TempDir(), "pkg", "mod")
	proxyDir := filepath.Join(hostModules, "cache", "download")

	if err := os.MkdirAll(proxyDir, 0o700); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Setenv("GOMODCACHE", hostModules)

	policy, err := createSandboxPolicy(t.Context(), workspace, home, t.TempDir(), configuredPaths{}, 0)
	if err != nil {
		t.Skipf("the sandbox cannot enforce the Go cache policy here: %v", err)
	}

	cacheDir := filepath.Join(home, ".cache")
	if slices.Contains(policy.Write, home) {
		t.Errorf("the Go caches made the shell home writable: %v", policy.Write)
	}
	if !slices.Contains(policy.Write, cacheDir) {
		t.Errorf("the shell cache is not writable: %v", policy.Write)
	}
	if !slices.Contains(policy.Read, proxyDir) {
		t.Errorf("got readable paths %v, want %s", policy.Read, proxyDir)
	}

	wantEnvironment := map[string]string{
		"GOCACHE":    filepath.Join(cacheDir, goBuildCacheDir),
		"GOMODCACHE": filepath.Join(cacheDir, goModuleCacheDir),
		"GOPROXY":    (&url.URL{Scheme: "file", Path: proxyDir}).String(),
		"GOSUMDB":    "off",
	}
	for name, want := range wantEnvironment {
		if got := policy.SetEnv[name]; got != want {
			t.Errorf("got %s %q, want %q", name, got, want)
		}
	}
}

func TestAMalformedGoModuleCacheIsRejected(t *testing.T) {
	t.Setenv("GOMODCACHE", "modules")

	if _, err := goModuleCache(); err == nil {
		t.Error("expected a relative Go module cache to be rejected")
	}
}

func TestNoPolicyGrantsMoreThanItsCapsAskFor(t *testing.T) {
	workspace := t.TempDir()
	home := t.TempDir()
	metadata := filepath.Join(workspace, ".git")

	if err := os.MkdirAll(metadata, 0o700); err != nil {
		t.Fatal(err)
	}

	for _, currentCaps := range everyCap {
		granted := writablePaths(workspace, home, currentCaps)

		if !currentCaps.has(capWrite) && slices.Contains(granted, workspace) {
			t.Errorf("%s: the tree is writable without w, got %v", currentCaps.Flags(), granted)
		}

		if !currentCaps.has(capWrite) && !currentCaps.has(capGit) && len(granted) > 0 {
			t.Errorf("%s: something is writable without w or g, got %v", currentCaps.Flags(), granted)
		}

		if !currentCaps.has(capGit) && slices.Contains(granted, metadata) {
			t.Errorf("%s: the metadata is writable without g, got %v", currentCaps.Flags(), granted)
		}

		if currentCaps.has(capWrite) && !slices.Contains(granted, workspace) {
			t.Errorf("%s: the tree is not writable with w, got %v", currentCaps.Flags(), granted)
		}
	}
}

func TestAReadOnlyShellMayChangeOnlyItsCache(t *testing.T) {
	workspace := t.TempDir()
	home := t.TempDir()
	cacheDir := filepath.Join(home, ".cache")

	policy, err := createSandboxPolicy(t.Context(), workspace, home, t.TempDir(), configuredPaths{}, 0)
	if err != nil {
		t.Skipf("the sandbox cannot enforce the shell cache policy here: %v", err)
	}

	if !slices.Equal(policy.Write, []string{cacheDir, sandbox.TmpDir}) {
		t.Errorf("got writable paths %v, want only the shell cache and scratch", policy.Write)
	}
	if !slices.Contains(policy.Read, home) {
		t.Errorf("got readable paths %v, want the shell home", policy.Read)
	}
}

func TestACommitOnlyShellMayChangeTheHistoryAlone(t *testing.T) {
	workspace := t.TempDir()
	home := t.TempDir()
	metadata := filepath.Join(workspace, ".git")

	if err := os.MkdirAll(metadata, 0o700); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	policy, err := createSandboxPolicy(t.Context(), workspace, home, t.TempDir(), configuredPaths{}, capGit)
	if err != nil {
		t.Skipf("the sandbox cannot enforce a writable policy here: %v", err)
	}

	if !slices.Contains(policy.Write, metadata) {
		t.Errorf("expected the metadata to be writable, got %v", policy.Write)
	}

	if slices.Contains(policy.Write, workspace) {
		t.Errorf("expected the tree itself to be read-only, got %v", policy.Write)
	}

	if !slices.Contains(policy.Read, workspace) {
		t.Errorf("expected the tree to be readable, got %v", policy.Read)
	}
}

func TestAnExactMetadataWritePathIsProtectedFromTheShell(t *testing.T) {
	repository := t.TempDir()
	target := filepath.Join(repository, ".git", "config")
	if err := os.Mkdir(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "shared")
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{target, alias} {
		policy, err := protectedPolicy(sandbox.Policy{Write: []string{path}}, []string{path})
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(policy.Read, path) {
			t.Errorf("expected %s to be protected, got %v", path, policy.Read)
		}
	}
}

func TestEveryExistingRepositoryIsProtectedFromTheShell(t *testing.T) {
	workspace := t.TempDir()
	home := t.TempDir()
	additional := t.TempDir()
	metadataPaths := []string{
		filepath.Join(workspace, ".git"),
		filepath.Join(workspace, "nested", ".git"),
		filepath.Join(additional, "nested", ".git"),
	}
	for _, metadata := range metadataPaths {
		if err := os.MkdirAll(metadata, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	policy, err := createSandboxPolicy(t.Context(), workspace, home, t.TempDir(), configuredPaths{
		Write: []string{additional},
	}, capWrite)
	if err != nil {
		t.Skipf("the sandbox cannot enforce a writable policy here: %v", err)
	}

	for _, metadata := range metadataPaths {
		if !slices.Contains(policy.Read, metadata) {
			t.Errorf("expected %s to be protected, got %v", metadata, policy.Read)
		}
	}
}

func TestACommitOnlyShellWithNoRepositoryChangesNothing(t *testing.T) {
	policy, err := createSandboxPolicy(t.Context(), t.TempDir(), t.TempDir(), t.TempDir(), configuredPaths{}, capGit)
	if err != nil {
		t.Skipf("the sandbox cannot enforce a policy here: %v", err)
	}

	if policy.Writable() {
		t.Errorf("got writable paths %v", policy.Write)
	}
}

func TestAWorkspaceCannotBeGivenWhenResuming(t *testing.T) {
	opts := InputOpts{Session: "one", WorkspaceDir: "somewhere"}
	if _, err := opts.parse(); err == nil {
		t.Error("expected an error")
	}
}

func TestAResumedConversationMayBeGrantedSomethingElse(t *testing.T) {
	opts := InputOpts{Session: "one", Caps: "rx"}
	settledOptions, err := opts.parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if settledOptions.caps.has(capWrite) {
		t.Error("expected writing to be held back")
	}
}

func conversationFixture(t *testing.T, hasSession bool, currentCaps caps) *conversation {
	t.Helper()

	log, err := store.Create(t.TempDir(), store.Meta{Model: "gpt"})
	if err != nil {
		t.Fatal(err)
	}

	if hasSession {
		if err := log.Event(agent.Event{Kind: agent.Prompt, Text: "hello"}); err != nil {
			t.Fatal(err)
		}
	}

	t.Cleanup(func() { _ = log.Close() })

	return &conversation{
		log:          log,
		workspaceDir: "/tmp/somewhere",
		mode:         NewMode(currentCaps),
	}
}

func TestStartingAgainNamesTheSessionAndKeepsTheMode(t *testing.T) {
	self := conversationFixture(t, true, capRead|capWrite|capShell)

	want := []string{"-r", self.log.ID(), "--caps", "rxw"}

	if got := self.restartArguments(); !slices.Equal(got, want) {
		t.Errorf("expected %v, got %v", want, got)
	}
}

func TestStartingAgainAsksForWhateverWasSwappedMidConversation(t *testing.T) {
	self := conversationFixture(t, true, capRead|capWrite|capShell)

	self.mode.Toggle(capWrite)
	self.mode.Toggle(capGit)

	want := []string{"-r", self.log.ID(), "--caps", "rxg"}

	if got := self.restartArguments(); !slices.Equal(got, want) {
		t.Errorf("expected %v, got %v", want, got)
	}
}

func TestStartingAgainWithNothingStoredKeepsTheWorkspace(t *testing.T) {
	self := conversationFixture(t, false, capRead|capWrite|capShell)

	want := []string{"--workspace", "/tmp/somewhere", "--caps", "rxw"}

	if got := self.restartArguments(); !slices.Equal(got, want) {
		t.Errorf("expected %v, got %v", want, got)
	}
}
