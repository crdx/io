package bash_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"crdx.org/io/internal/file"
	"crdx.org/io/internal/sandbox"
	"crdx.org/io/tool"
	"crdx.org/io/toolbox/bash"
)

func TestMain(m *testing.M) {
	sandbox.Init()
	os.Exit(m.Run())
}

func testRoot(t *testing.T) (*file.Root, string) {
	t.Helper()

	if err := sandbox.AvailableAtAll(); err != nil {
		t.Skipf("landlock is unavailable: %v", err)
	}

	directory := t.TempDir()

	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Cleanup(func() { _ = root.Close() })

	return file.New(root, allowAll), directory
}

func fixedShell(root *file.Root, policy func() sandbox.Policy) tool.Tool {
	return bash.New(
		root,
		func() bool { return !policy().Writable() },
		func(context.Context) (sandbox.Policy, error) { return policy(), nil },
		sandbox.NewProcesses(false),
	)
}

func exec(t *testing.T, root *file.Root, directory string, arguments string) (string, error) {
	t.Helper()

	output, _, err := execWithStats(t, root, directory, arguments)
	return output, err
}

func execWithStats(
	t *testing.T,
	root *file.Root,
	directory string,
	arguments string,
) (string, tool.Statistics, error) {
	t.Helper()

	policy := bash.ProtectedPolicy(sandbox.Policy{
		Write:   []string{directory},
		Env:     []string{"PATH"},
		Timeout: 10 * time.Second,
	})

	if err := sandbox.Supported(t.Context(), policy); err != nil {
		t.Skipf("the sandbox cannot enforce this policy: %v", err)
	}

	call, err := fixedShell(root, func() sandbox.Policy { return policy }).Parse(arguments)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output, err := call.Exec(t.Context())
	stats, _ := tool.Stats(call)
	return output, stats, err
}

func TestTheToolIsCalledExec(t *testing.T) {
	root, directory := testRoot(t)

	if name := fixedShell(root, func() sandbox.Policy { return sandbox.Policy{Write: []string{directory}} }).Name(); name != "bash" {
		t.Errorf("got %q, want %q", name, "bash")
	}
}

func TestOutputIsReturned(t *testing.T) {
	root, directory := testRoot(t)

	output, stats, err := execWithStats(t, root, directory, `{"command": "echo hello"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.TrimSpace(output) != "hello" {
		t.Errorf("got %q, want %q", output, "hello")
	}
	if stats.Lines != 1 || stats.Bytes != int64(len(output)) || stats.TotalBytes != stats.Bytes {
		t.Errorf("expected one line and %d returned and total bytes, got %+v", len(output), stats)
	}
}

func TestAnEmptyCommandIsRefused(t *testing.T) {
	root, directory := testRoot(t)

	if _, err := exec(t, root, directory, `{"command": "   "}`); err == nil {
		t.Errorf("the empty command was accepted")
	}
}

func TestAFailureCarriesItsExitStatus(t *testing.T) {
	root, directory := testRoot(t)

	output, err := exec(t, root, directory, `{"command": "echo no; exit 3"}`)
	if !errors.Is(err, bash.ErrCommandFailed) {
		t.Fatalf("expected the failure to be reported, got %v", err)
	}

	if !strings.HasPrefix(output, "exited 3:\n") {
		t.Errorf("got %q, want it to lead with the status and a colon", output)
	}
}

func TestADenialIsExplained(t *testing.T) {
	root, directory := testRoot(t)

	output, err := exec(t, root, directory, `{"command": "echo x > /etc/passwd"}`)
	if !errors.Is(err, bash.ErrCommandFailed) {
		t.Fatalf("expected the failure to be reported, got %v", err)
	}

	if !strings.Contains(output, "sandbox") {
		t.Errorf("got %q, want it to mention the sandbox", output)
	}
}

func TestAnOrdinaryFailureIsNotBlamedOnTheSandbox(t *testing.T) {
	root, directory := testRoot(t)

	output, err := exec(t, root, directory, `{"command": "exit 1"}`)
	if !errors.Is(err, bash.ErrCommandFailed) {
		t.Fatalf("expected the failure to be reported, got %v", err)
	}

	if strings.Contains(output, "sandbox") {
		t.Errorf("got %q, want no mention of the sandbox", output)
	}
}

func TestACommandIsRenderedOnOneLine(t *testing.T) {
	for name, command := range map[string]string{
		"newline":        "echo one\necho two",
		"carriage":       "echo one\recho two",
		"tab":            "echo\tone",
		"windows":        "echo one\r\necho two",
		"trailing":       "echo one\n",
		"blank between":  "echo one\n\n\necho two",
		"leading indent": "  echo one\n    echo two",
	} {
		renderedCommand, _ := bash.Render(bash.Args{Command: command})

		if strings.ContainsAny(renderedCommand, "\n\r\t") {
			t.Errorf("%s: expected one line, got %q", name, renderedCommand)
		}
	}
}

func TestACommandRenderingIsMarkedAsBash(t *testing.T) {
	root, _ := testRoot(t)
	call, err := fixedShell(root, func() sandbox.Policy { return sandbox.Policy{} }).Parse(`{"command":"echo one"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	syntaxCall, ok := call.(tool.SyntaxCall)
	if !ok || syntaxCall.Syntax() != "bash" {
		t.Errorf("expected bash syntax, got %T", call)
	}
}

func TestCommandLinesAreSeparatedOnOneLine(t *testing.T) {
	for command, want := range map[string]string{
		"echo one\necho two":      "echo one; echo two",
		"echo one;\necho two":     "echo one; echo two",
		"echo one &&\necho two":   "echo one && echo two",
		"echo one |\ngrep one":    "echo one | grep one",
		"if true; then\necho one": "if true; then echo one",
		"echo one\n\n\necho two":  "echo one; echo two",
	} {
		renderedCommand, _ := bash.Render(bash.Args{Command: command})
		if renderedCommand != want {
			t.Errorf("%q: got %q, want %q", command, renderedCommand, want)
		}
	}
}

func TestACommandOverSeveralLinesSaysHowMany(t *testing.T) {
	for command, want := range map[string]string{
		"echo one":                  "",
		"echo one\n":                "",
		"echo one\necho two":        "2 lines",
		"echo one\necho two\nls -l": "3 lines",
	} {
		if _, detail := bash.Render(bash.Args{Command: command}); detail != want {
			t.Errorf("%q: expected %q, got %q", command, want, detail)
		}
	}
}

func repository(t *testing.T, directory string) string {
	t.Helper()

	metadata := filepath.Join(directory, ".git")

	if err := os.Mkdir(metadata, 0o750); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := os.WriteFile(filepath.Join(metadata, "HEAD"), []byte("intact"), 0o600); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return metadata
}

func TestARepositoryIsReadableRatherThanWritable(t *testing.T) {
	_, directory := testRoot(t)
	metadata := repository(t, directory)

	policy := bash.ProtectedPolicy(sandbox.Policy{Write: []string{directory}})

	if !slices.Contains(policy.Read, metadata) {
		t.Errorf("got %v, want it to hold %s back", policy.Read, metadata)
	}

	if slices.Contains(policy.Write, metadata) {
		t.Errorf("got %v, want no write of %s", policy.Write, metadata)
	}
}

func TestAWorkspaceWithoutARepositoryIsLeftAsItIs(t *testing.T) {
	_, directory := testRoot(t)

	if policy := bash.ProtectedPolicy(sandbox.Policy{Write: []string{directory}}); len(policy.Read) > 0 {
		t.Errorf("got %v, want nothing held back", policy.Read)
	}
}

func TestARepositoryCannotBeClobbered(t *testing.T) {
	root, directory := testRoot(t)
	metadata := repository(t, directory)

	if err := sandbox.Supported(t.Context(), bash.ProtectedPolicy(sandbox.Policy{Write: []string{directory}})); err != nil {
		t.Skipf("the sandbox cannot hold a repository back: %v", err)
	}

	for _, command := range []string{
		`{"command": "echo clobbered > .git/HEAD"}`,
		`{"command": "rm -rf .git"}`,
		`{"command": "touch .git/index"}`,
	} {
		output, err := exec(t, root, directory, command)
		if err != nil && !errors.Is(err, bash.ErrCommandFailed) {
			t.Fatalf("unexpected error: %v", err) // the refusal itself is a failed command
		}

		if !strings.Contains(output, "Read-only file system") {
			t.Errorf("%s: got %q, want the filesystem to have refused it", command, output)
		}
	}

	content, err := os.ReadFile(filepath.Join(metadata, "HEAD")) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(content) != "intact" {
		t.Errorf("got %q, want %q", content, "intact")
	}
}

func TestAPolicyIsPassedThroughAsItIs(t *testing.T) {
	root, directory := testRoot(t)
	metadata := repository(t, directory)

	policy := sandbox.Policy{
		Write:   []string{directory},
		Env:     []string{"PATH"},
		Timeout: 10 * time.Second,
	}

	if err := sandbox.Supported(t.Context(), policy); err != nil {
		t.Skipf("the sandbox cannot enforce this policy: %v", err)
	}

	call, err := fixedShell(root, func() sandbox.Policy { return policy }).
		Parse(`{"command": "echo clobbered > .git/HEAD"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := call.Exec(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(metadata, "HEAD")) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(string(content), "clobbered") {
		t.Errorf("expected an unprotected policy to let the write through, got %q", content)
	}
}

func TestTheToolIsNeverConcurrent(t *testing.T) {
	root, directory := testRoot(t)

	if fixedShell(root, func() sandbox.Policy { return sandbox.Policy{Write: []string{directory}} }).Concurrent() {
		t.Errorf("a shell command is not safe to run alongside others")
	}
}

func TestTheToolAsksWhetherItChangesAnythingEachTime(t *testing.T) {
	root, directory := testRoot(t)

	readOnly := true
	shell := bash.New(
		root,
		func() bool { return readOnly },
		func(context.Context) (sandbox.Policy, error) {
			return sandbox.Policy{Write: []string{directory}}, nil
		},
		sandbox.NewProcesses(false),
	)

	if !shell.ReadOnly() {
		t.Errorf("a shell whose caller grants nowhere to write changes nothing")
	}

	readOnly = false

	if shell.ReadOnly() {
		t.Errorf("a shell whose caller grants somewhere to write may change something")
	}
}

func TestACancelledContextStopsTheCommand(t *testing.T) {
	root, directory := testRoot(t)

	ctx, stop := context.WithCancel(t.Context())

	policy := sandbox.Policy{Write: []string{directory}, Env: []string{"PATH"}}
	if err := sandbox.Supported(t.Context(), bash.ProtectedPolicy(policy)); err != nil {
		t.Skipf("the sandbox cannot enforce this policy: %v", err)
	}

	call, err := fixedShell(root, func() sandbox.Policy { return policy }).Parse(`{"command": "sleep 60"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	time.AfterFunc(100*time.Millisecond, stop)

	startedAt := time.Now()

	if _, err := call.Exec(ctx); err == nil {
		t.Error("expected a cancelled command to be an error")
	}

	if took := time.Since(startedAt); took > 10*time.Second {
		t.Errorf("expected the command to have been stopped, took %s", took)
	}
}

func allowAll(string) error { return nil }

func TestThePolicyIsAskedForEveryCommand(t *testing.T) {
	root, directory := testRoot(t)

	writable := true

	grantedPolicy := sandbox.Policy{
		Write:   []string{directory},
		Env:     []string{"PATH"},
		Timeout: 10 * time.Second,
	}

	withheld := sandbox.Policy{
		Read:    []string{directory},
		Env:     []string{"PATH"},
		Timeout: 10 * time.Second,
	}

	if err := sandbox.Supported(t.Context(), bash.ProtectedPolicy(grantedPolicy)); err != nil {
		t.Skipf("the sandbox cannot enforce this policy: %v", err)
	}

	built := fixedShell(root, func() sandbox.Policy {
		if writable {
			return grantedPolicy
		}

		return withheld
	})

	run := func() string {
		call, err := built.Parse(`{"command":"echo x > file"}`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		output, _ := call.Exec(t.Context())

		return output
	}

	if output := run(); strings.Contains(output, "denied") {
		t.Errorf("expected the write to be allowed, got %q", output)
	}

	writable = false

	if output := run(); !strings.Contains(output, "denied") {
		t.Errorf("expected the write to be refused, got %q", output)
	}

	if !built.ReadOnly() {
		t.Error("expected the tool to say it changes nothing while the policy grants no write")
	}
}
