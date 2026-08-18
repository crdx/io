package sandbox_test

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"crdx.org/io/internal/sandbox"
)

// TestMain makes the test binary its own sandbox stub, which is what any program offering the tool
// has to do. A run started by Run never reaches the tests.
func TestMain(m *testing.M) {
	sandbox.Init()
	os.Exit(m.Run())
}

func TestPolicyModifiersDoNotChangeTheSourcePolicy(t *testing.T) {
	source := sandbox.Policy{
		Read:   []string{"read"},
		Write:  []string{"write"},
		Exec:   []string{"exec"},
		SetEnv: map[string]string{"EXISTING": "original"},
	}

	modified := source.WithRead("more-read").WithWrite("more-write").WithExec("more-exec").WithSetEnv("ADDED", "value")
	modified.Read[0] = "changed"
	modified.Write[0] = "changed"
	modified.Exec[0] = "changed"
	modified.SetEnv["EXISTING"] = "changed"

	if source.Read[0] != "read" {
		t.Errorf("read paths changed to %v", source.Read)
	}
	if source.Write[0] != "write" {
		t.Errorf("write paths changed to %v", source.Write)
	}
	if source.Exec[0] != "exec" {
		t.Errorf("executable paths changed to %v", source.Exec)
	}
	if source.SetEnv["EXISTING"] != "original" {
		t.Errorf("environment changed to %v", source.SetEnv)
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

	if policy.TmpDir == "" { // a scratch bound over /tmp hides every test directory beneath it
		policy.Write = append(policy.Write, directory)
	}

	policy.Env = append(policy.Env, "PATH")

	if policy.Timeout == 0 {
		policy.Timeout = 10 * time.Second
	}

	if err := sandbox.Supported(t.Context(), policy); err != nil {
		t.Skipf("the sandbox cannot enforce this policy: %v", err)
	}

	var result sandbox.Result
	var err error
	if policy.Background {
		processes := sandbox.NewProcesses(true)
		defer func() { _, _ = processes.Disable() }()
		result, err = processes.Run(context.Background(), directory, command, policy)
	} else {
		result, err = sandbox.Run(context.Background(), directory, command, policy)
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return result
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

func TestAGrantedPathIsStillNotWritable(t *testing.T) {
	grantedDirectory := t.TempDir()

	result := run(t, t.TempDir(), "echo no > "+filepath.Join(grantedDirectory, "file"), sandbox.Policy{
		Read: []string{grantedDirectory},
	})

	if result.Code == 0 {
		t.Errorf("the write was allowed")
	}
}

// A read-only grant is worth nothing if the file can still be emptied, which is what TRUNCATE is
// for and why the minimum landlock version is what it is.
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

const readOnly = "Read-only file system" // what a refusal by the mount rather than the ruleset says

// A read-only path inside a writable one is the whole reason the sandbox takes a mount namespace.
// Landlock grants a path every right that any rule along the way to it grants, so the inner rule
// alone would be worth nothing.
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

// Holding one path back is no use if it costs the rest of the workspace.
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

// A held path is still readable, or a command could not so much as look at what it may not change.
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

// The command is handed the namespace, so it must not be handed the privilege that made it.
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
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
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

	listener, err := net.Listen("unix", path)
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

func TestAUnixSocketMayReachAHostServiceUnderAWritableGrant(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "host.sock")

	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Skipf("host Unix sockets are unavailable: %v", err)
	}
	defer func() { _ = listener.Close() }()

	result := run(t, directory, "python3 -c '"+
		"import socket; s=socket.socket(socket.AF_UNIX); s.connect(\"host.sock\")'",
		sandbox.Policy{Background: true})

	if strings.Contains(result.Output, "python3: command not found") {
		t.Skip("python3 is unavailable")
	}
	if strings.Contains(result.Output, "Address family not supported") {
		t.Skip("the kernel cannot grant pathname Unix sockets")
	}
	if result.Code != 0 {
		t.Errorf("a service under a writable grant was unreachable: %q", result.Output)
	}
}

func TestCommandsMayTalkOverAUnixSocketInsideTheSandbox(t *testing.T) {
	command := `python3 - <<'PY'
import socket
server = socket.socket(socket.AF_UNIX)
server.bind("local.sock")
server.listen()
client = socket.socket(socket.AF_UNIX)
client.connect("local.sock")
print("connected")
PY`

	result := run(t, t.TempDir(), command, sandbox.Policy{})
	if strings.Contains(result.Output, "python3: command not found") {
		t.Skip("python3 is unavailable")
	}
	if strings.Contains(result.Output, "Address family not supported") {
		t.Skip("the kernel cannot isolate pathname Unix sockets")
	}
	if result.Code != 0 || !strings.Contains(result.Output, "connected") {
		t.Errorf("commands in the sandbox could not connect: %q", result.Output)
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

// A machine keeps its secrets in /etc as readily as its loader configuration, so what a command
// needs of it is named file by file and the rest is left where it is.
func TestOnlyTheNamedPartsOfEtcAreReachable(t *testing.T) {
	if result := run(t, t.TempDir(), "cat /etc/passwd", sandbox.Policy{}); result.Code != 0 {
		t.Errorf("a command cannot resolve a user: %q", result.Output)
	}

	if result := run(t, t.TempDir(), "ls /etc", sandbox.Policy{}); result.Code == 0 {
		t.Errorf("the whole of /etc was listed: %q", result.Output)
	}
}

// Attaching a scratch replaces what was at /tmp, so a grant beneath it is gone by the time it is
// applied. Landlock says only that the path is missing, which is true and no help at all.
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

// The scratch itself is granted as /tmp, which is where it is attached and what it is for.
func TestTheScratchItselfIsNotRefused(t *testing.T) {
	policy := sandbox.Policy{TmpDir: t.TempDir(), Write: []string{sandbox.TmpDir}}

	_, err := sandbox.Run(t.Context(), t.TempDir(), "true", policy)
	if err != nil && strings.Contains(err.Error(), "granted but unreachable") {
		t.Errorf("expected the scratch to be granted, got %v", err)
	}
}

// TMPDIR is advisory and a good deal of software ignores it, so the scratch is what a command
// insisting on /tmp writes into, and what it left there last time is what it finds there now.
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

// Processor time is not wall clock: a command that spins hits this while one that waits does not.
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
