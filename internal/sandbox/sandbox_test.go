package sandbox_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"crdx.org/hereduck"
	"crdx.org/io/internal/sandbox"
)

const (
	opensslConfigurationPath    = "/etc/ssl/openssl.cnf"
	ownerDeathHelperVariable    = "IO_SANDBOX_OWNER_DEATH_HELPER"
	ownerDeathDirectoryVariable = "IO_SANDBOX_OWNER_DEATH_DIRECTORY"
)

func TestMain(m *testing.M) {
	sandbox.Init()
	os.Exit(m.Run())
}

func TestPolicyModifiersDoNotChangeTheSourcePolicy(t *testing.T) {
	source := sandbox.Policy{
		Read:   []string{"read"},
		Write:  []string{"write"},
		SetEnv: map[string]string{"EXISTING": "original"},
	}

	modified := source.WithRead("more-read").WithWrite("more-write").WithSetEnv("ADDED", "value")
	modified.Read[0] = "changed"
	modified.Write[0] = "changed"
	modified.SetEnv["EXISTING"] = "changed"

	if source.Read[0] != "read" {
		t.Errorf("read paths changed to %v", source.Read)
	}
	if source.Write[0] != "write" {
		t.Errorf("write paths changed to %v", source.Write)
	}
	if source.SetEnv["EXISTING"] != "original" {
		t.Errorf("environment changed to %v", source.SetEnv)
	}
}

func TestWithoutReadRemovesPathsWithoutChangingTheSourcePolicy(t *testing.T) {
	source := sandbox.Policy{Read: []string{"remove", "keep"}}
	modified := source.WithoutRead("remove")

	if !slices.Equal(modified.Read, []string{"keep"}) {
		t.Errorf("got readable paths %v, want only keep", modified.Read)
	}
	modified.Read[0] = "changed"
	if !slices.Equal(source.Read, []string{"remove", "keep"}) {
		t.Errorf("source readable paths changed to %v", source.Read)
	}
}

func requireLandlock(t *testing.T) {
	t.Helper()

	if err := sandbox.AvailableAtAll(); err != nil {
		t.Skipf("landlock is unavailable: %v", err)
	}
}

func run(t *testing.T, directory string, command string, policy sandbox.Policy) sandbox.Result {
	t.Helper()

	if policy.TmpDir == "" {
		policy.Write = append(policy.Write, directory)
	}

	policy.Env = append(policy.Env, "PATH")

	if policy.Timeout == 0 {
		policy.Timeout = 10 * time.Second
	}

	if err := sandbox.Supported(t.Context(), policy); err != nil {
		t.Skipf("the sandbox cannot enforce this policy: %v", err)
	}

	result, err := sandbox.Run(context.Background(), directory, command, policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return result
}

type concurrentRun struct {
	index  int
	result sandbox.Result
	err    error
}

func runConcurrently(
	t *testing.T,
	directories [2]string,
	commands [2]string,
	policies [2]sandbox.Policy,
) [2]sandbox.Result {
	t.Helper()

	for i, policy := range policies {
		if err := sandbox.Supported(t.Context(), policy); err != nil {
			t.Skipf("the sandbox cannot enforce policy %d: %v", i, err)
		}
	}

	started := make(chan struct{})
	finished := make(chan concurrentRun, len(policies))
	for i := range policies {
		go func() {
			<-started
			result, err := sandbox.Run(t.Context(), directories[i], commands[i], policies[i])
			finished <- concurrentRun{index: i, result: result, err: err}
		}()
	}
	close(started)

	var results [2]sandbox.Result
	for range policies {
		got := <-finished
		if got.err != nil {
			t.Fatalf("command %d failed to run: %v", got.index, got.err)
		}
		results[got.index] = got.result
	}

	return results
}

func TestACommandRunsAndReportsItsOutput(t *testing.T) {
	result := run(t, t.TempDir(), "echo hello", sandbox.Policy{})

	if strings.TrimSpace(result.Output) != "hello" {
		t.Errorf("got %q, want %q", result.Output, "hello")
	}

	if result.Code != 0 {
		t.Errorf("got exit status %d, want 0", result.Code)
	}
}

func TestACommandCombinesOutputInTheOrderItWasWritten(t *testing.T) {
	command := "printf one; printf two >&2; printf three; printf four >&2"
	result := run(t, t.TempDir(), command, sandbox.Policy{})

	if result.Code != 0 || result.Output != "onetwothreefour" {
		t.Errorf("got exit status %d with output %q", result.Code, result.Output)
	}
}

func TestACommandMayStartANewSession(t *testing.T) {
	result := run(t, t.TempDir(), `setsid sh -c 'printf session-ok'`, sandbox.Policy{})
	if result.Code != 0 || result.Output != "session-ok" {
		t.Errorf("got exit status %d with output %q", result.Code, result.Output)
	}
}

func TestANewSessionCannotOutliveItsCommand(t *testing.T) {
	directory := t.TempDir()
	marker := filepath.Join(directory, "detached")
	command := "setsid sh -c 'printf started > " + marker +
		"; sleep 0.2; printf escaped >> " + marker + "' >/dev/null 2>&1 & " +
		"for _ in {1..100}; do test -s " + marker + " && break; sleep 0.01; done; test -s " + marker

	result := run(t, directory, command, sandbox.Policy{})
	if result.Code != 0 {
		t.Fatalf("the detached session did not start: exit status %d with output %q", result.Code, result.Output)
	}

	content, err := os.ReadFile(marker) //nolint:gosec // reading the test's own marker is intended
	if err != nil || string(content) != "started" {
		t.Fatalf("got marker %q, %v before the command ended", content, err)
	}

	time.Sleep(400 * time.Millisecond)
	content, err = os.ReadFile(marker) //nolint:gosec // reading the test's own marker is intended
	if err != nil || string(content) != "started" {
		t.Errorf("the detached session survived: got marker %q, %v", content, err)
	}
}

func TestADetachedSessionCannotHoldCommandOutputOpen(t *testing.T) {
	startedAt := time.Now()
	result := run(t, t.TempDir(), "setsid sleep 30 & printf finished", sandbox.Policy{})

	if result.Code != 0 || result.Output != "finished" {
		t.Errorf("got exit status %d with output %q", result.Code, result.Output)
	}
	if duration := time.Since(startedAt); duration > 2*time.Second {
		t.Errorf("the detached session held the command open for %s", duration)
	}
}

func TestAFailedCommandCannotLeaveANewSessionBehind(t *testing.T) {
	directory := t.TempDir()
	marker := filepath.Join(directory, "failed-detached")
	command := "setsid sh -c 'sleep 0.2; printf escaped > " + marker + "' & exit 23"

	result := run(t, directory, command, sandbox.Policy{})
	if result.Code != 23 {
		t.Fatalf("got exit status %d with output %q, want 23", result.Code, result.Output)
	}

	time.Sleep(400 * time.Millisecond)
	if content, err := os.ReadFile(marker); err == nil { //nolint:gosec // reading the test's own marker is intended
		t.Errorf("the detached session survived: got marker %q", content)
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("could not inspect the marker: %v", err)
	}
}

func TestACommandDiesWhenItsOwnerIsKilled(t *testing.T) {
	if os.Getenv(ownerDeathHelperVariable) != "" {
		runOwnerDeathHelper(t)
		return
	}

	directory := t.TempDir()
	policy := sandbox.Policy{
		Write:   []string{directory},
		Env:     []string{"PATH"},
		Timeout: 10 * time.Second,
	}
	if err := sandbox.Supported(t.Context(), policy); err != nil {
		t.Skipf("the sandbox cannot enforce this policy: %v", err)
	}

	owner := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestACommandDiesWhenItsOwnerIsKilled$") //nolint:gosec // rerunning this test binary is intended
	owner.Env = append(
		os.Environ(),
		ownerDeathHelperVariable+"=1",
		ownerDeathDirectoryVariable+"="+directory,
	)
	if err := owner.Start(); err != nil {
		t.Fatalf("could not start the command owner: %v", err)
	}
	defer func() { _ = owner.Process.Kill() }()

	ready := filepath.Join(directory, "ready")
	activity := filepath.Join(directory, "activity")
	waitForFileContents(t, ready, "ready")

	if err := owner.Process.Kill(); err != nil {
		t.Fatalf("could not kill the command owner: %v", err)
	}
	if err := owner.Wait(); err == nil {
		t.Error("the killed command owner exited successfully")
	}

	time.Sleep(100 * time.Millisecond)
	content, err := os.ReadFile(activity) //nolint:gosec // reading the test's own marker is intended
	if err != nil {
		t.Fatalf("could not read the command's activity: %v", err)
	}
	assertFileContentsRemain(t, activity, string(content))
}

func runOwnerDeathHelper(t *testing.T) {
	t.Helper()

	directory := os.Getenv(ownerDeathDirectoryVariable)
	ready := filepath.Join(directory, "ready")
	activity := filepath.Join(directory, "activity")
	command := "printf x > " + activity + "; printf ready > " + ready +
		"; for _ in {1..30}; do sleep 0.05; printf x >> " + activity + "; done"
	policy := sandbox.Policy{
		Write:   []string{directory},
		Env:     []string{"PATH"},
		Timeout: 10 * time.Second,
	}

	if _, err := sandbox.Run(t.Context(), directory, command, policy); err != nil {
		t.Fatalf("the owned command did not run: %v", err)
	}
}

func TestACancelledContextStartsNoCommand(t *testing.T) {
	directory := t.TempDir()
	marker := filepath.Join(directory, "never-started")
	policy := sandbox.Policy{Write: []string{directory}, Env: []string{"PATH"}}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := sandbox.Run(ctx, directory, "printf started > "+marker, policy)
	if err == nil {
		t.Error("the command with an already cancelled context was allowed to run")
	}
	if content, err := os.ReadFile(marker); err == nil { //nolint:gosec // reading the test's own marker is intended
		t.Errorf("the cancelled command wrote %q", content)
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("could not inspect the command marker: %v", err)
	}
}

func TestCancellingACommandKillsItsDetachedSessions(t *testing.T) {
	directory := t.TempDir()
	marker := filepath.Join(directory, "cancelled-detached")
	policy := sandbox.Policy{
		Write:   []string{directory},
		Env:     []string{"PATH"},
		Timeout: 10 * time.Second,
	}
	if err := sandbox.Supported(t.Context(), policy); err != nil {
		t.Skipf("the sandbox cannot enforce this policy: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	finished := make(chan error, 1)
	go func() {
		_, err := sandbox.Run(ctx, directory, delayedDetachedWrite(marker), policy)
		finished <- err
	}()

	waitForFileContents(t, marker, "started")
	cancel()

	select {
	case err := <-finished:
		if err == nil || !strings.Contains(err.Error(), "command was stopped") {
			t.Errorf("got %v, want the command to report its cancellation", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the cancelled command did not stop")
	}

	assertFileContentsRemain(t, marker, "started")
}

func TestTimingOutACommandKillsItsDetachedSessions(t *testing.T) {
	directory := t.TempDir()
	marker := filepath.Join(directory, "timed-out-detached")
	policy := sandbox.Policy{
		Write:   []string{directory},
		Env:     []string{"PATH"},
		Timeout: 200 * time.Millisecond,
	}
	if err := sandbox.Supported(t.Context(), policy); err != nil {
		t.Skipf("the sandbox cannot enforce this policy: %v", err)
	}

	_, err := sandbox.Run(t.Context(), directory, delayedDetachedWrite(marker), policy)
	if err == nil || !strings.Contains(err.Error(), "did not finish within 200ms") {
		t.Errorf("got %v, want the command to report its timeout", err)
	}

	waitForFileContents(t, marker, "started")
	assertFileContentsRemain(t, marker, "started")
}

func delayedDetachedWrite(marker string) string {
	return "setsid sh -c 'printf started > " + marker +
		"; sleep 0.5; printf escaped >> " + marker + "' & sleep 30"
}

func waitForFileContents(t *testing.T, path string, want string) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		content, err := os.ReadFile(path) //nolint:gosec // reading the test's own marker is intended
		if err == nil && string(content) == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	content, err := os.ReadFile(path) //nolint:gosec // reading the test's own marker is intended
	t.Fatalf("got marker %q, %v, want %q", content, err, want)
}

func assertFileContentsRemain(t *testing.T, path string, want string) {
	t.Helper()

	time.Sleep(700 * time.Millisecond)
	content, err := os.ReadFile(path) //nolint:gosec // reading the test's own marker is intended
	if err != nil || string(content) != want {
		t.Errorf("the detached session survived: got marker %q, %v", content, err)
	}
}

func TestACommandMayOpenAPseudoterminal(t *testing.T) {
	if _, err := os.Stat("/dev/ptmx"); err != nil {
		t.Skipf("no pseudoterminal device: %v", err)
	}

	scratch := t.TempDir()
	policy := sandbox.Policy{TmpDir: scratch, Write: []string{sandbox.TmpDir}}
	result := run(t, t.TempDir(), "exec 3<>/dev/ptmx", policy)
	if result.Code != 0 {
		t.Errorf("got exit status %d with output %q", result.Code, result.Output)
	}
}

func TestTheCallerMaySetAnEnvironmentVariable(t *testing.T) {
	result := run(t, t.TempDir(), `printf %s "$FIXED"`, sandbox.Policy{
		SetEnv: map[string]string{"FIXED": "chosen"},
	})

	if result.Output != "chosen" {
		t.Errorf("got %q, want %q", result.Output, "chosen")
	}
}

func TestASetEnvironmentVariableWinsOverTheParent(t *testing.T) {
	t.Setenv("FIXED", "parent")

	result := run(t, t.TempDir(), `printf %s "$FIXED"`, sandbox.Policy{
		Env:    []string{"FIXED"},
		SetEnv: map[string]string{"FIXED": "chosen"},
	})

	if result.Output != "chosen" {
		t.Errorf("got %q, want %q", result.Output, "chosen")
	}
}

func TestConcurrentCommandsKeepIndependentFilesystemPolicies(t *testing.T) {
	firstDirectory := t.TempDir()
	secondDirectory := t.TempDir()
	coordinationDirectory := t.TempDir()
	firstReady := filepath.Join(coordinationDirectory, "first-ready")
	secondReady := filepath.Join(coordinationDirectory, "second-ready")

	commands := [2]string{
		concurrentWriteCommand(firstReady, secondReady, firstDirectory, secondDirectory),
		concurrentWriteCommand(secondReady, firstReady, secondDirectory, firstDirectory),
	}
	policies := [2]sandbox.Policy{
		{
			Read:    []string{secondDirectory},
			Write:   []string{firstDirectory, coordinationDirectory},
			Env:     []string{"PATH"},
			Timeout: 10 * time.Second,
		},
		{
			Read:    []string{firstDirectory},
			Write:   []string{secondDirectory, coordinationDirectory},
			Env:     []string{"PATH"},
			Timeout: 10 * time.Second,
		},
	}

	results := runConcurrently(
		t,
		[2]string{firstDirectory, secondDirectory},
		commands,
		policies,
	)
	for i, result := range results {
		if result.Code != 0 {
			t.Errorf("command %d got exit status %d with output %q", i, result.Code, result.Output)
		}
	}

	for _, directory := range []string{firstDirectory, secondDirectory} {
		content, err := os.ReadFile(filepath.Join(directory, "own")) //nolint:gosec // reading the test's own file is intended
		if err != nil || string(content) != "own" {
			t.Errorf("got own file %q, %v", content, err)
		}
		if content, err := os.ReadFile(filepath.Join(directory, "crossed")); err == nil { //nolint:gosec // reading the test's own file is intended
			t.Errorf("another command crossed into the policy with %q", content)
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Errorf("could not inspect the crossed file: %v", err)
		}
	}
}

func concurrentWriteCommand(ready string, peerReady string, ownDirectory string, foreignDirectory string) string {
	return "touch " + ready + "; " +
		"for _ in {1..100}; do test -e " + peerReady + " && break; sleep 0.01; done; " +
		"test -e " + peerReady + " || exit 24; " +
		"printf own > " + filepath.Join(ownDirectory, "own") + "; " +
		"if printf crossed > " + filepath.Join(foreignDirectory, "crossed") + " 2>/dev/null; then exit 25; fi"
}

func TestConcurrentCommandsKeepIndependentEnvironmentsAndLimits(t *testing.T) {
	coordinationDirectory := t.TempDir()
	firstReady := filepath.Join(coordinationDirectory, "first-ready")
	secondReady := filepath.Join(coordinationDirectory, "second-ready")

	command := func(ready string, peerReady string) string {
		return "touch " + ready + "; " +
			"for _ in {1..100}; do test -e " + peerReady + " && break; sleep 0.01; done; " +
			`printf '%s %s %s' "$NAME" "$(ulimit -n)" "$(ulimit -u)"`
	}
	policies := [2]sandbox.Policy{
		{
			Write:     []string{coordinationDirectory},
			Env:       []string{"PATH"},
			SetEnv:    map[string]string{"NAME": "first"},
			Timeout:   10 * time.Second,
			OpenFiles: 64,
			Processes: 32,
		},
		{
			Write:     []string{coordinationDirectory},
			Env:       []string{"PATH"},
			SetEnv:    map[string]string{"NAME": "second"},
			Timeout:   10 * time.Second,
			OpenFiles: 128,
			Processes: 64,
		},
	}

	results := runConcurrently(
		t,
		[2]string{coordinationDirectory, coordinationDirectory},
		[2]string{command(firstReady, secondReady), command(secondReady, firstReady)},
		policies,
	)
	for i, want := range []string{"first 64 32", "second 128 64"} {
		if results[i].Code != 0 || results[i].Output != want {
			t.Errorf("command %d got exit status %d with output %q, want %q", i, results[i].Code, results[i].Output, want)
		}
	}
}

func TestTheWorkingDirectoryIsWritable(t *testing.T) {
	directory := t.TempDir()

	result := run(t, directory, "echo written > file", sandbox.Policy{})

	if result.Code != 0 {
		t.Fatalf("got exit status %d with output %q", result.Code, result.Output)
	}

	content, err := os.ReadFile(filepath.Join(directory, "file")) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.TrimSpace(string(content)) != "written" {
		t.Errorf("got %q, want %q", content, "written")
	}
}

func TestAGeneratedFileMayBeExecutedWhenGranted(t *testing.T) {
	directory := t.TempDir()

	result := run(t, directory,
		`printf '#!/bin/sh\necho ran\n' > built && chmod +x built && ./built`,
		sandbox.Policy{Exec: []string{directory}})

	if result.Code != 0 || !strings.Contains(result.Output, "ran") {
		t.Errorf("the generated file did not run: %q", result.Output)
	}
}

func TestOnlyAnExactlyGrantedFileMayBeExecuted(t *testing.T) {
	directory := t.TempDir()
	exact := filepath.Join(directory, "exact")
	sibling := filepath.Join(directory, "sibling")
	for path, content := range map[string]string{
		exact:   "#!/bin/sh\nprintf exact",
		sibling: "#!/bin/sh\nprintf sibling",
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o700); err != nil { //nolint:gosec // the test fixture must be executable
			t.Fatal(err)
		}
	}

	result := run(t, t.TempDir(), exact+"; "+sibling, sandbox.Policy{Exec: []string{exact}})
	if !strings.Contains(result.Output, "exact") || strings.Contains(result.Output, "sibling") {
		t.Errorf("the exact-file grant executed the wrong command: %q", result.Output)
	}
}

func TestAWriteOutsideThePolicyIsRefused(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "outside")

	result := run(t, t.TempDir(), "echo leaked > "+outside, sandbox.Policy{})

	if result.Code == 0 {
		t.Fatalf("the write was allowed")
	}

	if _, err := os.Stat(outside); err == nil {
		t.Errorf("the file was created despite the refusal")
	}
}

func TestAReadOutsideThePolicyIsRefused(t *testing.T) {
	directory := t.TempDir()
	secret := filepath.Join(directory, "secret")

	if err := os.WriteFile(secret, []byte("hidden"), 0o600); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := run(t, t.TempDir(), "cat "+secret, sandbox.Policy{})

	if result.Code == 0 || strings.Contains(result.Output, "hidden") {
		t.Errorf("the read was allowed: %q", result.Output)
	}
}

func TestAGrantedPathIsReadable(t *testing.T) {
	grantedDirectory := t.TempDir()

	if err := os.WriteFile(filepath.Join(grantedDirectory, "shared"), []byte("visible"), 0o600); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := run(t, t.TempDir(), "cat "+filepath.Join(grantedDirectory, "shared"), sandbox.Policy{
		Read: []string{grantedDirectory},
	})

	if !strings.Contains(result.Output, "visible") {
		t.Errorf("got %q, want it to contain %q", result.Output, "visible")
	}
}

func TestOnlyAnExactlyGrantedFileIsReadable(t *testing.T) {
	directory := t.TempDir()
	exact := filepath.Join(directory, "exact")
	sibling := filepath.Join(directory, "sibling")
	if err := os.WriteFile(exact, []byte("exact"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sibling, []byte("sibling"), 0o600); err != nil {
		t.Fatal(err)
	}

	result := run(t, t.TempDir(), "cat "+exact+"; cat "+sibling, sandbox.Policy{Read: []string{exact}})
	if !strings.Contains(result.Output, "exact") || strings.Contains(result.Output, "sibling") {
		t.Errorf("the exact-file grant exposed the wrong content: %q", result.Output)
	}
}

func TestOnlyAnExactlyGrantedFileIsWritable(t *testing.T) {
	directory := t.TempDir()
	exact := filepath.Join(directory, "exact")
	sibling := filepath.Join(directory, "sibling")
	if err := os.WriteFile(exact, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sibling, []byte("sibling"), 0o600); err != nil {
		t.Fatal(err)
	}

	run(t, t.TempDir(), "printf changed > "+exact+"; printf leaked > "+sibling,
		sandbox.Policy{Write: []string{exact}})
	content, err := os.ReadFile(exact) //nolint:gosec // reading the test's own file is intended
	if err != nil || string(content) != "changed" {
		t.Errorf("exact file got %q and %v", content, err)
	}
	content, err = os.ReadFile(sibling) //nolint:gosec // reading the test's own file is intended
	if err != nil || string(content) != "sibling" {
		t.Errorf("sibling got %q and %v", content, err)
	}
}

func TestAnExactReadGrantInsideAWriteGrantRemainsReadOnly(t *testing.T) {
	directory := t.TempDir()
	exact := filepath.Join(directory, "exact")
	sibling := filepath.Join(directory, "sibling")
	if err := os.WriteFile(exact, []byte("intact"), 0o600); err != nil {
		t.Fatal(err)
	}

	run(t, t.TempDir(), "printf blocked > "+exact+"; printf written > "+sibling,
		sandbox.Policy{Read: []string{exact}, Write: []string{directory}})
	content, err := os.ReadFile(exact) //nolint:gosec // reading the test's own file is intended
	if err != nil || string(content) != "intact" {
		t.Errorf("read-only file got %q and %v", content, err)
	}
	content, err = os.ReadFile(sibling) //nolint:gosec // reading the test's own file is intended
	if err != nil || string(content) != "written" {
		t.Errorf("writable sibling got %q and %v", content, err)
	}
}

func TestAGrantedPathIsStillNotWritable(t *testing.T) {
	grantedDirectory := t.TempDir()

	result := run(t, t.TempDir(), "echo no > "+filepath.Join(grantedDirectory, "file"), sandbox.Policy{
		Read: []string{grantedDirectory},
	})

	if result.Code == 0 {
		t.Errorf("the write was allowed")
	}
}

func TestAReadableFileCannotBeTruncated(t *testing.T) {
	grantedDirectory := t.TempDir()
	target := filepath.Join(grantedDirectory, "keep")

	if err := os.WriteFile(target, []byte("intact"), 0o600); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	run(t, t.TempDir(), ": > "+target, sandbox.Policy{Read: []string{grantedDirectory}})

	content, err := os.ReadFile(target) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(content) != "intact" {
		t.Errorf("the file was emptied")
	}
}

const readOnly = "Read-only file system"

func TestAReadPathInsideAWritePathIsNotWritable(t *testing.T) {
	directory := t.TempDir()
	protectedPath := filepath.Join(directory, "held")

	if err := os.Mkdir(protectedPath, 0o750); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	kept := filepath.Join(protectedPath, "kept")

	if err := os.WriteFile(kept, []byte("intact"), 0o600); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, command := range []string{
		"echo clobbered > held/kept",
		"rm held/kept",
		"touch held/new",
		"mkdir held/new",
		": > held/kept",
		"rm -rf held",
	} {
		result := run(t, directory, command, sandbox.Policy{Read: []string{protectedPath}})

		if result.Code == 0 {
			t.Errorf("%q was allowed", command)
		}

		if !strings.Contains(result.Output, readOnly) {
			t.Errorf("%q: got %q, want it to mention %q", command, result.Output, readOnly)
		}
	}

	if result := run(t, directory, "mv held elsewhere", sandbox.Policy{Read: []string{protectedPath}}); result.Code == 0 {
		t.Errorf("the held path was moved out of the way")
	}

	content, err := os.ReadFile(kept) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(content) != "intact" {
		t.Errorf("got %q, want %q", content, "intact")
	}
}

func TestTheRestOfTheWorkspaceIsStillWritable(t *testing.T) {
	directory := t.TempDir()
	protectedPath := filepath.Join(directory, "held")

	if err := os.Mkdir(protectedPath, 0o750); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := run(t, directory, "mkdir work && echo written > work/file && cat held/../work/file",
		sandbox.Policy{Read: []string{protectedPath}})

	if result.Code != 0 {
		t.Fatalf("got exit status %d with output %q", result.Code, result.Output)
	}

	if !strings.Contains(result.Output, "written") {
		t.Errorf("got %q, want it to contain %q", result.Output, "written")
	}
}

func TestAHeldPathIsStillReadable(t *testing.T) {
	directory := t.TempDir()
	protectedPath := filepath.Join(directory, "held")

	if err := os.Mkdir(protectedPath, 0o750); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := os.WriteFile(filepath.Join(protectedPath, "kept"), []byte("visible"), 0o600); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := run(t, directory, "cat held/kept", sandbox.Policy{Read: []string{protectedPath}})

	if !strings.Contains(result.Output, "visible") {
		t.Errorf("got %q, want it to contain %q", result.Output, "visible")
	}
}

func TestTheCommandCannotUndoWhatHoldsAPathBack(t *testing.T) {
	directory := t.TempDir()
	protectedPath := filepath.Join(directory, "held")

	if err := os.Mkdir(protectedPath, 0o750); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := run(t, directory, "umount held; echo clobbered > held/kept",
		sandbox.Policy{Read: []string{protectedPath}})

	if result.Code == 0 || !strings.Contains(result.Output, readOnly) {
		t.Errorf("the mount was undone: %q", result.Output)
	}

	if _, err := os.Stat(filepath.Join(protectedPath, "kept")); err == nil {
		t.Errorf("the file was written despite the refusal")
	}
}

func TestTheNetworkIsUnreachable(t *testing.T) {
	result := run(t, t.TempDir(), "exec 3<>/dev/tcp/1.1.1.1/80", sandbox.Policy{})

	if result.Code == 0 {
		t.Errorf("the connection was allowed")
	}
}

func TestLoopbackIsReachable(t *testing.T) {
	addresses := []struct {
		host   string
		family string
	}{
		{host: "127.0.0.1", family: "AF_INET"},
		{host: "::1", family: "AF_INET6"},
	}

	for _, address := range addresses {
		command := "python3 -c 'import socket; f=socket." + address.family + "; " +
			"s=socket.create_server((\"" + address.host + "\",0),family=f); " +
			"c=socket.socket(f); c.connect(s.getsockname()); print(\"connected\")'"
		result := run(t, t.TempDir(), command, sandbox.Policy{})

		if strings.Contains(result.Output, "python3: command not found") {
			t.Skip("python3 is unavailable")
		}
		if address.family == "AF_INET6" && strings.Contains(result.Output, "Address family not supported") {
			continue
		}
		if result.Code != 0 || !strings.Contains(result.Output, "connected") {
			t.Errorf("loopback %s was unreachable: %q", address.host, result.Output)
		}
	}
}

func TestDatagramsStayOnLoopback(t *testing.T) {
	command := "python3 -c 'import socket; " +
		"s=socket.socket(socket.AF_INET,socket.SOCK_DGRAM); s.bind((\"127.0.0.1\",0)); " +
		"c=socket.socket(socket.AF_INET,socket.SOCK_DGRAM); c.sendto(b\"ping\",s.getsockname()); " +
		"print(s.recvfrom(4)[0].decode())'"
	result := run(t, t.TempDir(), command, sandbox.Policy{})

	if strings.Contains(result.Output, "python3: command not found") {
		t.Skip("python3 is unavailable")
	}
	if result.Code != 0 || !strings.Contains(result.Output, "ping") {
		t.Errorf("loopback datagram failed: %q", result.Output)
	}
}

func TestHostLoopbackIsUnreachable(t *testing.T) {
	var config net.ListenConfig

	listener, err := config.Listen(t.Context(), "tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("host sockets are unavailable: %v", err)
	}
	defer func() { _ = listener.Close() }()

	host, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("could not split the host address: %v", err)
	}

	result := run(t, t.TempDir(), "exec 3<>/dev/tcp/"+host+"/"+port, sandbox.Policy{})
	if result.Code == 0 {
		t.Error("a service on the host loopback was reachable")
	}
}

func TestAUnixSocketCannotReachAHostService(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "host.sock")

	var config net.ListenConfig

	listener, err := config.Listen(t.Context(), "unix", path)
	if err != nil {
		t.Skipf("host Unix sockets are unavailable: %v", err)
	}
	defer func() { _ = listener.Close() }()

	result := run(t, t.TempDir(), "python3 -c '"+
		"import socket; s=socket.socket(socket.AF_UNIX); s.connect(\""+path+"\")'",
		sandbox.Policy{Read: []string{directory}})

	if strings.Contains(result.Output, "python3: command not found") {
		t.Skip("python3 is unavailable")
	}

	if result.Code == 0 {
		t.Error("a service on a host Unix socket was reachable")
	}
}

func unixSocketCommand(directory string) string {
	return `python3 - <<'PY'
import socket
server = socket.socket(socket.AF_UNIX)
server.bind("` + filepath.Join(directory, "local.sock") + `")
server.listen()
client = socket.socket(socket.AF_UNIX)
client.connect("` + filepath.Join(directory, "local.sock") + `")
print("connected")
PY`
}

func TestCommandsMayTalkOverAUnixSocketInTheScratch(t *testing.T) {
	scratch := t.TempDir()
	policy := sandbox.Policy{
		Write:   []string{sandbox.TmpDir},
		Sockets: []string{sandbox.TmpDir},
		TmpDir:  scratch,
	}

	result := run(t, t.TempDir(), unixSocketCommand(sandbox.TmpDir), policy)
	if strings.Contains(result.Output, "python3: command not found") {
		t.Skip("python3 is unavailable")
	}
	if strings.Contains(result.Output, "Address family not supported") {
		t.Skip("this kernel refuses Unix sockets outright, having no way to isolate them")
	}
	if result.Code != 0 || !strings.Contains(result.Output, "connected") {
		t.Errorf("commands in the sandbox could not connect: %q", result.Output)
	}
}

func TestAUnixSocketOutsideTheNamedPathsIsRefused(t *testing.T) {
	requireLandlock(t)

	directory := t.TempDir()
	policy := sandbox.Policy{Write: []string{directory}}

	result := run(t, directory, unixSocketCommand(directory), policy)
	if strings.Contains(result.Output, "python3: command not found") {
		t.Skip("python3 is unavailable")
	}
	if result.Code == 0 {
		t.Errorf("a writable path nothing named resolved a socket: %q", result.Output)
	}
}

func TestNetworkControlSocketsAreRefused(t *testing.T) {
	command := `python3 - <<'PY'
import socket
for family in (socket.AF_NETLINK, socket.AF_PACKET):
    try:
        socket.socket(family, socket.SOCK_RAW)
    except OSError:
        continue
    raise AssertionError(f"family {family} was allowed")
PY`

	result := run(t, t.TempDir(), command, sandbox.Policy{})
	if strings.Contains(result.Output, "python3: command not found") {
		t.Skip("python3 is unavailable")
	}
	if result.Code != 0 {
		t.Errorf("a network control socket was allowed: %q", result.Output)
	}
}

func TestRawIPSocketsAreRefused(t *testing.T) {
	result := run(t, t.TempDir(), "python3 -c '"+
		"import socket; socket.socket(socket.AF_INET,socket.SOCK_RAW,socket.IPPROTO_ICMP)'",
		sandbox.Policy{})

	if strings.Contains(result.Output, "python3: command not found") {
		t.Skip("python3 is unavailable")
	}
	if result.Code == 0 {
		t.Error("a raw IP socket was allowed")
	}
}

func TestTheCommandCannotReconfigureLoopback(t *testing.T) {
	command := `python3 - <<'PY'
import fcntl
import socket
import struct
s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
fcntl.ioctl(s, 0x8914, struct.pack("16sH14x", b"lo", 1))
PY`

	result := run(t, t.TempDir(), command, sandbox.Policy{})
	if strings.Contains(result.Output, "python3: command not found") {
		t.Skip("python3 is unavailable")
	}
	if result.Code == 0 {
		t.Error("the loopback interface was reconfigured")
	}
}

func TestALocalSocketPairStillWorks(t *testing.T) {
	result := run(t, t.TempDir(), "python3 -c '"+
		"import socket; socket.socketpair(); print(\"made\")'", sandbox.Policy{})

	if strings.Contains(result.Output, "python3: command not found") {
		t.Skip("python3 is unavailable")
	}

	if result.Code != 0 || !strings.Contains(result.Output, "made") {
		t.Errorf("a local socket pair was refused: %q", result.Output)
	}
}

func TestOnlyTheNamedPartsOfEtcAreReachable(t *testing.T) {
	if result := run(t, t.TempDir(), "cat /etc/passwd", sandbox.Policy{}); result.Code != 0 {
		t.Errorf("a command cannot resolve a user: %q", result.Output)
	}

	if result := run(t, t.TempDir(), "ls /etc", sandbox.Policy{}); result.Code == 0 {
		t.Errorf("the whole of /etc was listed: %q", result.Output)
	}
}

var virtualResolverFiles = map[string]string{
	"/etc/resolv.conf": hereduck.D(`
		# managed by oh
		nameserver 127.0.0.1
	`),

	"/etc/hosts": hereduck.D(`
		127.0.0.1 localhost
		::1 localhost ip6-localhost ip6-loopback
	`),

	"/etc/nsswitch.conf": hereduck.D(`
		passwd: files
		group: files
		shadow: files
		hosts: files dns
		networks: files
		protocols: files
		services: files
		ethers: files
		rpc: files
	`),
}

func TestVirtualResolverurationReplacesTheHostFiles(t *testing.T) {
	policy := sandbox.Policy{VirtualResolver: true}

	for path, contents := range virtualResolverFiles {
		result := run(t, t.TempDir(), "cat "+path, policy)
		if result.Code != 0 {
			t.Errorf("%s is unreadable: %q", path, result.Output)
			continue
		}
		if result.Output != contents {
			t.Errorf("got %s as %q, want %q", path, result.Output, contents)
		}
	}
}

func TestTheHostResolverFilesStayHiddenWithoutTheirPolicy(t *testing.T) {
	for path := range virtualResolverFiles {
		if path == "/etc/nsswitch.conf" {
			continue
		}

		if result := run(t, t.TempDir(), "cat "+path, sandbox.Policy{}); result.Code == 0 {
			t.Errorf("%s was readable without asking for it: %q", path, result.Output)
		}
	}
}

func TestTheVirtualResolverFilesAreRefusedByTheMountRatherThanLandlock(t *testing.T) {
	policy := sandbox.Policy{VirtualResolver: true}

	for path := range virtualResolverFiles {
		result := run(t, t.TempDir(), "printf changed > "+path, policy)

		if !strings.Contains(result.Output, "Read-only file system") {
			t.Errorf(
				"%s was refused with %q, want the read-only mount to be the one refusing it",
				path, strings.TrimSpace(result.Output),
			)
		}
	}
}

func TestTheVirtualResolverFilesCannotBeChanged(t *testing.T) {
	policy := sandbox.Policy{VirtualResolver: true}

	for path := range virtualResolverFiles {
		if result := run(t, t.TempDir(), "printf changed > "+path, policy); result.Code == 0 {
			t.Errorf("%s was overwritten: %q", path, result.Output)
		}
		if result := run(t, t.TempDir(), ": > "+path, policy); result.Code == 0 {
			t.Errorf("%s was truncated: %q", path, result.Output)
		}
	}

	for path, contents := range virtualResolverFiles {
		if result := run(t, t.TempDir(), "cat "+path, policy); result.Output != contents {
			t.Errorf("%s now reads %q, want %q", path, result.Output, contents)
		}
	}
}

func TestOpenSSLConfigurationIsReadableWithoutExposingItsDirectory(t *testing.T) {
	if _, err := os.Stat(opensslConfigurationPath); err != nil {
		t.Skipf("OpenSSL configuration is unavailable: %v", err)
	}

	result := run(t, t.TempDir(), "cat "+opensslConfigurationPath+" >/dev/null", sandbox.Policy{})
	if result.Code != 0 {
		t.Errorf("OpenSSL configuration is unreadable: %q", result.Output)
	}

	result = run(t, t.TempDir(), "ls /etc/ssl", sandbox.Policy{})
	if result.Code == 0 {
		t.Errorf("the OpenSSL configuration directory was listed: %q", result.Output)
	}
}

func TestOpenSSLCanLoadItsSystemConfiguration(t *testing.T) {
	if _, err := os.Stat(opensslConfigurationPath); err != nil {
		t.Skipf("OpenSSL configuration is unavailable: %v", err)
	}
	if _, err := exec.LookPath("openssl"); err != nil {
		t.Skipf("OpenSSL is unavailable: %v", err)
	}

	result := run(t, t.TempDir(), "openssl list -providers", sandbox.Policy{})
	if result.Code != 0 {
		t.Fatalf("OpenSSL could not load its system configuration: %q", result.Output)
	}
	if !strings.Contains(result.Output, "default") {
		t.Errorf("OpenSSL did not load the default provider: %q", result.Output)
	}
}

func TestAGrantTheScratchWouldCoverIsRefused(t *testing.T) {
	policy := sandbox.Policy{
		TmpDir: t.TempDir(),
		Write:  []string{filepath.Join(sandbox.TmpDir, "elsewhere")},
	}

	_, err := sandbox.Run(t.Context(), t.TempDir(), "true", policy)
	if err == nil || !strings.Contains(err.Error(), "granted but unreachable") {
		t.Errorf("expected the covered grant to be named, got %v", err)
	}
}

func TestTheScratchItselfIsNotRefused(t *testing.T) {
	policy := sandbox.Policy{TmpDir: t.TempDir(), Write: []string{sandbox.TmpDir}}

	_, err := sandbox.Run(t.Context(), t.TempDir(), "true", policy)
	if err != nil && strings.Contains(err.Error(), "granted but unreachable") {
		t.Errorf("expected the scratch to be granted, got %v", err)
	}
}

func TestWhatACommandWritesToTmpLandsInTheScratch(t *testing.T) {
	scratch := t.TempDir()

	left := filepath.Join(scratch, "kept")
	if err := os.WriteFile(left, []byte("kept\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	policy := sandbox.Policy{TmpDir: scratch, Write: []string{sandbox.TmpDir}}
	result := run(t, t.TempDir(), "cat /tmp/kept && echo written > /tmp/file", policy)

	if result.Code != 0 {
		t.Fatalf("got exit status %d with output %q", result.Code, result.Output)
	}

	content, err := os.ReadFile(filepath.Join(scratch, "file")) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.TrimSpace(string(content)) != "written" {
		t.Errorf("got %q, want %q", content, "written")
	}
}

func TestCommandsSharingAScratchShareItsContents(t *testing.T) {
	scratch := t.TempDir()
	policy := sandbox.Policy{TmpDir: scratch, Write: []string{sandbox.TmpDir}}

	written := run(t, t.TempDir(), "printf durable > /tmp/state", policy)
	if written.Code != 0 {
		t.Fatalf("the first command failed with exit status %d and output %q", written.Code, written.Output)
	}

	read := run(t, t.TempDir(), "cat /tmp/state", policy)
	if read.Code != 0 || read.Output != "durable" {
		t.Errorf("the second command got exit status %d with output %q", read.Code, read.Output)
	}
}

func TestCommandsWithDifferentScratchesCannotSeeEachOthersContents(t *testing.T) {
	firstPolicy := sandbox.Policy{TmpDir: t.TempDir(), Write: []string{sandbox.TmpDir}}
	secondPolicy := sandbox.Policy{TmpDir: t.TempDir(), Write: []string{sandbox.TmpDir}}

	written := run(t, t.TempDir(), "printf private > /tmp/state", firstPolicy)
	if written.Code != 0 {
		t.Fatalf("the first command failed with exit status %d and output %q", written.Code, written.Output)
	}

	hidden := run(t, t.TempDir(), "test ! -e /tmp/state", secondPolicy)
	if hidden.Code != 0 {
		t.Errorf("the second command saw the first command's scratch: %q", hidden.Output)
	}

	kept := run(t, t.TempDir(), "cat /tmp/state", firstPolicy)
	if kept.Code != 0 || kept.Output != "private" {
		t.Errorf("the first scratch got exit status %d with output %q", kept.Code, kept.Output)
	}
}

func TestAnExecutableBuiltInTmpMayRunWhenGranted(t *testing.T) {
	scratch := t.TempDir()
	policy := sandbox.Policy{
		TmpDir: scratch,
		Write:  []string{sandbox.TmpDir},
		Exec:   []string{sandbox.TmpDir},
	}

	result := run(t, t.TempDir(),
		`printf '#!/bin/sh\necho ran\n' > /tmp/built && chmod +x /tmp/built && /tmp/built`, policy)

	if result.Code != 0 || !strings.Contains(result.Output, "ran") {
		t.Errorf("the generated file did not run: %q", result.Output)
	}
}

func TestACommandThatOverrunsIsStopped(t *testing.T) {
	requireLandlock(t)

	directory := t.TempDir()
	policy := sandbox.Policy{
		Write:   []string{directory},
		Env:     []string{"PATH"},
		Timeout: 200 * time.Millisecond,
	}

	if err := sandbox.Supported(t.Context(), policy); err != nil {
		t.Skipf("the sandbox cannot enforce this policy: %v", err)
	}

	_, err := sandbox.Run(context.Background(), directory, "sleep 30", policy)

	if err == nil {
		t.Fatalf("the command was allowed to finish")
	}

	if !strings.Contains(err.Error(), "did not finish") {
		t.Errorf("got %q, want it to mention the timeout", err)
	}
}

func TestAFileMayNotGrowPastTheLimit(t *testing.T) {
	directory := t.TempDir()

	result := run(t, directory, "dd if=/dev/zero of=big bs=1024 count=64", sandbox.Policy{
		FileSize: 8 * 1024,
	})

	if result.Code == 0 {
		t.Fatalf("the write was allowed")
	}

	fileInfo, err := os.Stat(filepath.Join(directory, "big"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fileInfo.Size() > 8*1024 {
		t.Errorf("got %d bytes, want no more than %d", fileInfo.Size(), 8*1024)
	}
}

func TestACommandMayNotBurnMoreProcessorTimeThanTheLimit(t *testing.T) {
	result := run(t, t.TempDir(), "while :; do :; done", sandbox.Policy{
		CPUTime: time.Second,
		Timeout: 30 * time.Second,
	})

	if result.Code == 0 {
		t.Errorf("the command was allowed to run on")
	}
}

func TestDescriptorsAreLimited(t *testing.T) {
	result := run(t, t.TempDir(), "ulimit -n", sandbox.Policy{OpenFiles: 64})

	if strings.TrimSpace(result.Output) != "64" {
		t.Errorf("got %q, want %q", result.Output, "64")
	}
}

func TestCompletedCommandsLeakNoDescriptors(t *testing.T) {
	directory := t.TempDir()
	policy := sandbox.Policy{
		Write:   []string{directory},
		Env:     []string{"PATH"},
		Timeout: 10 * time.Second,
	}
	if err := sandbox.Supported(t.Context(), policy); err != nil {
		t.Skipf("the sandbox cannot enforce this policy: %v", err)
	}

	before := openDescriptorCount(t)
	for range 10 {
		result, err := sandbox.Run(t.Context(), directory, "true", policy)
		if err != nil || result.Code != 0 {
			t.Fatalf("command got exit status %d with error %v and output %q", result.Code, err, result.Output)
		}
	}
	after := openDescriptorCount(t)

	if after != before {
		t.Errorf("got %d open descriptors after the commands, started with %d", after, before)
	}
}

func openDescriptorCount(t *testing.T) int {
	t.Helper()

	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Skipf("the process descriptor directory is unavailable: %v", err)
	}
	return len(entries)
}

func TestProcessesAreLimited(t *testing.T) {
	result := run(t, t.TempDir(), "ulimit -u", sandbox.Policy{Processes: 64})

	if strings.TrimSpace(result.Output) != "64" {
		t.Errorf("got %q, want %q", result.Output, "64")
	}
}

const outputLimit = 8 << 20

func TestOutputBeyondWhatIsKeptDoesNotStopTheCommand(t *testing.T) {
	command := fmt.Sprintf("yes .........| head -c %d", 3*outputLimit)
	result := run(t, t.TempDir(), command, sandbox.Policy{})

	if result.Code != 0 {
		t.Errorf("got exit status %d, want a command writing past the cap to finish", result.Code)
	}

	if len(result.Output) != outputLimit {
		t.Errorf("got %d bytes, want the output held at %d", len(result.Output), outputLimit)
	}
}

func TestALimitThatCouldNeverBeMetIsRefused(t *testing.T) {
	requireLandlock(t)

	directory := t.TempDir()

	_, err := sandbox.Run(context.Background(), directory, "true", sandbox.Policy{
		Write:   []string{directory},
		CPUTime: time.Millisecond,
	})

	if err == nil || !strings.Contains(err.Error(), "no time at all") {
		t.Errorf("got %v, want a complaint about the limit", err)
	}
}

func TestAPolicyNamingAMissingPathIsRefused(t *testing.T) {
	requireLandlock(t)

	directory := t.TempDir()

	_, err := sandbox.Run(context.Background(), directory, "true", sandbox.Policy{
		Read: []string{filepath.Join(directory, "nowhere")},
	})

	if err == nil || !strings.Contains(err.Error(), "do not exist") {
		t.Errorf("got %v, want a complaint about the missing path", err)
	}
}

func TestAPolicyWithALimitThatIsNotALimitIsRefusedBeforeAnythingRuns(t *testing.T) {
	for _, policy := range []sandbox.Policy{{FileSize: -1}, {OpenFiles: -1}} {
		if _, err := sandbox.Run(t.Context(), t.TempDir(), "true", policy); err == nil {
			t.Errorf("%+v was accepted", policy)
		}
	}
}

func TestAGrantThroughAModelSymlinkRefusesTheCommand(t *testing.T) {
	requireLandlock(t)

	home := t.TempDir()
	victim := t.TempDir()
	planted := filepath.Join(home, ".cache")
	if err := os.Symlink(victim, planted); err != nil {
		t.Fatal(err)
	}

	directory := t.TempDir()
	command := "touch " + filepath.Join(victim, "pwned")

	_, err := sandbox.Run(t.Context(), directory, command, sandbox.Policy{
		Write: []string{home, planted},
		Env:   []string{"PATH"},
	})
	if err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Errorf("got %v, want the planted link to refuse the command", err)
	}

	if _, statErr := os.Stat(filepath.Join(victim, "pwned")); statErr == nil {
		t.Error("the redirected grant wrote outside the sandbox")
	}
}

func TestARealCachePathStillGrantsWrite(t *testing.T) {
	home := t.TempDir()
	cache := filepath.Join(home, ".cache")
	if err := os.Mkdir(cache, 0o700); err != nil {
		t.Fatal(err)
	}

	directory := t.TempDir()
	kept := filepath.Join(cache, "kept")
	result := run(t, directory, "printf kept > "+kept, sandbox.Policy{Write: []string{home, cache}})
	if result.Code != 0 {
		t.Fatalf("got exit status %d with output %q", result.Code, result.Output)
	}

	content, err := os.ReadFile(kept) //nolint:gosec // the test's own path
	if err != nil || strings.TrimSpace(string(content)) != "kept" {
		t.Errorf("got %q and %v", content, err)
	}
}
