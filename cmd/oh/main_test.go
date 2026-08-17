package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"crdx.org/duckopt/v2"
	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/session"
	"crdx.org/io/internal/file"
	"crdx.org/io/internal/sandbox"
)

func bind(t *testing.T, arguments ...string) Opts {
	t.Helper()

	originalArgs := os.Args
	t.Cleanup(func() { os.Args = originalArgs })

	os.Args = append([]string{"oh"}, arguments...)

	bound, err := duckopt.Bind[Opts](usage, "$0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return *bound
}

func parseOptions(t *testing.T, arguments ...string) invocation {
	t.Helper()

	settledOptions, err := bind(t, arguments...).invocation()
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
	parsedOptions := parseOptions(t, "-c", "r", "-d", "somewhere")

	if parsedOptions.caps != capRead {
		t.Errorf("expected reading alone, got %s", parsedOptions.caps.Letters())
	}

	if parsedOptions.workspace != "somewhere" {
		t.Errorf("expected the directory, got %q", parsedOptions.workspace)
	}

	if parsedOptions := parseOptions(t, "--resume"); !parsedOptions.resume {
		t.Errorf("expected resume, got %v", parsedOptions.resume)
	}

	id := "0347juX1xcrL9W0QKJe0cs"

	if parsedOptions := parseOptions(t, "-r", id); parsedOptions.session != id || !parsedOptions.resume { // and a prompt beside it matches no pattern
		t.Errorf("expected the session, got %q", parsedOptions.session)
	}
}

// What is left over is what to open the conversation with, so it need not be quoted to be one
// thing: a shell hands it over a word at a time and it goes back together as it was typed.
func TestWhateverIsLeftOverIsTheFirstThingSaid(t *testing.T) {
	parsedOptions := parseOptions(t, "why", "does", "the", "spinner", "stutter")

	if parsedOptions.initialMessage != "why does the spinner stutter" {
		t.Errorf("expected the words back as one, got %q", parsedOptions.initialMessage)
	}

	if parsedOptions.workspace != "." {
		t.Errorf("expected the current directory, got %q", parsedOptions.workspace)
	}
}

func TestTheWorkingDirectoryIsNotTakenFromThePrompt(t *testing.T) {
	parsedOptions := parseOptions(t, "read", "main.go", "-d", "/tmp")

	if parsedOptions.workspace != "/tmp" {
		t.Errorf("expected the directory to come from the option, got %q", parsedOptions.workspace)
	}

	if parsedOptions.initialMessage != "read main.go" {
		t.Errorf("expected the rest to be the prompt, got %q", parsedOptions.initialMessage)
	}
}

// Nothing said is rwx: history and background processes are only ever opened on purpose.
func TestTheDefaultCapabilitiesAreEverythingButTheHistory(t *testing.T) {
	parsedOptions := parseOptions(t)

	if got := parsedOptions.caps.Letters(); got != "rwx" {
		t.Errorf("expected rwx, got %q", got)
	}

	if parsedOptions.initialMessage != "" {
		t.Errorf("expected nothing said, got %q", parsedOptions.initialMessage)
	}
}

// Capabilities are a set, so the order the letters are written in says nothing, and a letter that
// names none of them is a typo worth refusing rather than ignoring.
func TestCapabilitiesAreReadAsTheLettersTheyAreSpelledWith(t *testing.T) {
	for _, capString := range []string{"rwxgb", "bgxwr", "wxgb"} {
		currentCaps, err := Caps(capString)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", capString, err)
		}

		if got := currentCaps.Letters(); got != capLetters {
			t.Errorf("%s: expected it written back as %s, got %q", capString, capLetters, got)
		}
	}

	if _, err := Caps("rwz"); err == nil {
		t.Error("expected a letter naming no capability to be refused")
	}
}

// Reading is granted whether it was asked for or not, and is written back either way.
func TestReadingIsAlwaysGranted(t *testing.T) {
	grantedCaps, err := Caps("")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if got := grantedCaps.Letters(); got != "r" {
		t.Errorf("expected r, got %q", got)
	}
}

func TestPromptSeparatesTheWorkspaceFromTmp(t *testing.T) {
	system := prompt("/workspace", capRead)

	for _, clause := range []string{
		"scratch space and is always read-write",
		"The workspace is read-only",
		"HOME may be read-only",
	} {
		if !strings.Contains(system, clause) {
			t.Errorf("expected %q in %q", clause, system)
		}
	}

	if strings.Contains(system, "including /tmp") {
		t.Errorf("the workspace mode still claims to include /tmp: %q", system)
	}
}

// The shell is offered whatever is granted, so a conversation held without it has the same tools as
// one held with it, and finds out at the call rather than never hearing of the tool at all.
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

	shell := confinedShell(t.TempDir(), t.TempDir(), t.TempDir(), mode, files, processes)

	if shell.Name() != "exec" {
		t.Errorf("expected the shell to be offered as exec, got %q", shell.Name())
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

func TestTheShellMayUseItsWorkspaceAndHome(t *testing.T) {
	workspace := t.TempDir()
	home := t.TempDir()

	policy, err := shellPolicy(t.Context(), workspace, home, t.TempDir(), capWrite)
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

	if policy.SetEnv["TMPDIR"] != sandbox.TmpDir {
		t.Errorf("got TMPDIR %q, want %q", policy.SetEnv["TMPDIR"], sandbox.TmpDir)
	}
}

func TestBackgroundModeReachesTheShellPolicy(t *testing.T) {
	policy, err := shellPolicy(t.Context(), t.TempDir(), t.TempDir(), t.TempDir(), capBackground)
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

	readOnly, err := shellPolicy(t.Context(), workspace, home, tmp, 0)
	if err != nil {
		t.Skipf("the sandbox cannot enforce a read-only policy here: %v", err)
	}
	if !slices.Contains(readOnly.Write, sandbox.TmpDir) {
		t.Errorf("expected writable %s, got %v", sandbox.TmpDir, readOnly.Write)
	}

	readWrite, err := shellPolicy(t.Context(), workspace, home, tmp, capWrite)
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

func TestCommandsOnThePathMayBeExecuted(t *testing.T) {
	workspace := t.TempDir()
	installDir := t.TempDir()
	t.Setenv("PATH", installDir)

	if paths := execPaths(workspace); !slices.Contains(paths, installDir) {
		t.Errorf("got %v, want it to include %s", paths, installDir)
	}
}

func TestAnExistingGoModuleCacheIsReadable(t *testing.T) {
	workspace := t.TempDir()
	home := t.TempDir()
	modules := filepath.Join(t.TempDir(), "pkg", "mod")

	if err := os.MkdirAll(modules, 0o700); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Setenv("GOMODCACHE", modules)

	policy, _ := shellPolicy(t.Context(), workspace, home, t.TempDir(), capWrite)

	if !slices.Contains(policy.Read, modules) {
		t.Errorf("got readable paths %v, want %s", policy.Read, modules)
	}

	if policy.SetEnv["GOMODCACHE"] != modules {
		t.Errorf("got GOMODCACHE %q, want %q", policy.SetEnv["GOMODCACHE"], modules)
	}
}

func TestAnUnresolvableGoModuleCacheIsIgnored(t *testing.T) {
	directory := t.TempDir()
	t.Chdir(directory)
	if err := os.Remove(directory); err != nil {
		t.Fatalf("could not remove the working directory: %v", err)
	}

	t.Setenv("GOMODCACHE", "modules")

	if got := goModuleCache(); got != "" {
		t.Errorf("got %q, want no module cache", got)
	}
}

// A shell that cannot be confined is never offered unconfined. The tool is still there to be
// called, and says so when it is, rather than the machine refusing to start over it.
func TestAPolicyThatCannotBeEnforcedIsAnError(t *testing.T) {
	workspace := t.TempDir()
	home := t.TempDir()

	for _, currentCaps := range everyCap {
		policy, err := shellPolicy(t.Context(), workspace, home, t.TempDir(), currentCaps)

		if enforceable := sandbox.Supported(t.Context(), policy); (err == nil) != (enforceable == nil) {
			t.Errorf(
				"expected the policy handed back to be enforceable exactly where there is no "+
					"error, got error %v against %v", err, enforceable,
			)
		}
	}
}

func TestAReadOnlyShellCannotChangeItsHome(t *testing.T) {
	workspace := t.TempDir()
	home := t.TempDir()

	policy, _ := shellPolicy(t.Context(), workspace, home, t.TempDir(), 0)

	if policy.Writable() {
		t.Errorf("got writable paths %v", policy.Write)
	}

	if !slices.Contains(policy.Read, home) {
		t.Errorf("got readable paths %v, want the shell home", policy.Read)
	}
}

// A commit-only mode grants the metadata and nothing else of the tree, which is what lets a
// conversation store the work it has done without editing a line of it.
func TestACommitOnlyShellMayChangeTheHistoryAlone(t *testing.T) {
	workspace := t.TempDir()
	home := t.TempDir()
	metadata := filepath.Join(workspace, ".git")

	if err := os.MkdirAll(metadata, 0o700); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	policy, err := shellPolicy(t.Context(), workspace, home, t.TempDir(), capGit)
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

func TestEveryExistingRepositoryIsProtectedFromTheShell(t *testing.T) {
	workspace := t.TempDir()
	home := t.TempDir()
	for _, metadata := range []string{
		filepath.Join(workspace, ".git"),
		filepath.Join(workspace, "nested", ".git"),
	} {
		if err := os.MkdirAll(metadata, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	policy, err := shellPolicy(t.Context(), workspace, home, t.TempDir(), capWrite)
	if err != nil {
		t.Skipf("the sandbox cannot enforce a writable policy here: %v", err)
	}

	for _, metadata := range []string{
		filepath.Join(workspace, ".git"),
		filepath.Join(workspace, "nested", ".git"),
	} {
		if !slices.Contains(policy.Read, metadata) {
			t.Errorf("expected %s to be protected, got %v", metadata, policy.Read)
		}
	}
}

// There is nothing to commit into without a repository, so the mode grants the shell nothing.
func TestACommitOnlyShellWithNoRepositoryChangesNothing(t *testing.T) {
	policy, err := shellPolicy(t.Context(), t.TempDir(), t.TempDir(), t.TempDir(), capGit)
	if err != nil {
		t.Skipf("the sandbox cannot enforce a policy here: %v", err)
	}

	if policy.Writable() {
		t.Errorf("got writable paths %v", policy.Write)
	}
}

// What the usage allows to be written and what may be true at once are different things, and these
// are the second: each of them parses, and none of them means anything.
func TestArgumentsThatContradictEachOtherAreRefused(t *testing.T) {
	for name, opts := range map[string]Opts{
		"a picker and a directory":  {Resume: true, Workspace: "somewhere"},
		"a session and a directory": {Resume: true, Session: "one", Workspace: "somewhere"},
	} {
		if _, err := opts.invocation(); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

// What is granted is settled each time the harness starts rather than by the session, so picking a
// stored conversation up under something else is the whole point rather than a contradiction.
func TestAResumedConversationMayBeGrantedSomethingElse(t *testing.T) {
	for name, opts := range map[string]Opts{
		"a session and a cap": {Resume: true, Session: "one", Caps: "rx"},
		"a picker and a cap":  {Resume: true, Caps: "rx"},
	} {
		settledOptions, err := opts.invocation()
		if err != nil {
			t.Errorf("%s: unexpected error: %v", name, err)
		}

		if settledOptions.caps.has(capWrite) {
			t.Errorf("%s: expected writing to be held back", name)
		}
	}
}

func conversationFixture(t *testing.T, hasSession bool, currentCaps caps) *conversation {
	t.Helper()

	log, err := session.Create(t.TempDir(), session.Header{Model: "gpt"})
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
		log:       log,
		workspace: "/tmp/somewhere",
		mode:      NewMode(currentCaps),
	}
}

// Starting again carries the conversation over by naming it, and asks for the mode it ended up in
// rather than the one it was started with.
func TestStartingAgainNamesTheSessionAndKeepsTheMode(t *testing.T) {
	self := conversationFixture(t, true, capRead|capWrite|capShell)

	want := []string{"--resume", self.log.ID(), "--caps", "rwx"}

	if got := self.restartArguments(); !slices.Equal(got, want) {
		t.Errorf("expected %v, got %v", want, got)
	}
}

func TestStartingAgainAsksForWhateverWasSwappedMidConversation(t *testing.T) {
	self := conversationFixture(t, true, capRead|capWrite|capShell)

	self.mode.Toggle(capWrite)
	self.mode.Toggle(capGit)

	want := []string{"--resume", self.log.ID(), "--caps", "rxg"}

	if got := self.restartArguments(); !slices.Equal(got, want) {
		t.Errorf("expected %v, got %v", want, got)
	}
}

// A conversation nothing has been said in has no session to carry over, so the workspace is what
// keeps the new run where this one stood.
func TestStartingAgainWithNothingStoredKeepsTheWorkspace(t *testing.T) {
	self := conversationFixture(t, false, capRead|capWrite|capShell)

	want := []string{"--workspace", "/tmp/somewhere", "--caps", "rwx"}

	if got := self.restartArguments(); !slices.Equal(got, want) {
		t.Errorf("expected %v, got %v", want, got)
	}
}
