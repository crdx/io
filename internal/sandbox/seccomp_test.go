package sandbox

import (
	"errors"
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

func TestTheSocketFilterAllowsOnlyNamespacedNetworking(t *testing.T) {
	target, err := architecture()
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name             string
		allowUnixSockets bool
		unixAction       uint32
	}{
		{name: "before Unix socket isolation", unixAction: actionErrno | uint32(unix.EAFNOSUPPORT)},
		{name: "with Unix socket isolation", allowUnixSockets: true, unixAction: actionAllow},
	} {
		t.Run(test.name, func(t *testing.T) {
			filter, err := buildFilter(test.allowUnixSockets)
			if err != nil {
				t.Fatal(err)
			}

			for family, want := range map[uint32]uint32{
				unix.AF_INET:    actionAllow,
				unix.AF_INET6:   actionAllow,
				unix.AF_UNIX:    test.unixAction,
				unix.AF_PACKET:  actionErrno | uint32(unix.EAFNOSUPPORT),
				unix.AF_NETLINK: actionErrno | uint32(unix.EAFNOSUPPORT),
			} {
				if got := evaluate(filter, target.audit, target.socket, family); got != want {
					t.Errorf("family %d: got action %#x, want %#x", family, got, want)
				}
			}

			if got := evaluate(filter, target.audit, target.socket+1, unix.AF_UNIX); got != actionAllow {
				t.Errorf("an unrelated syscall got action %#x, want allow", got)
			}
			if got := evaluate(filter, 0, target.socket, unix.AF_INET); got != actionKillProcess {
				t.Errorf("the wrong architecture got action %#x, want kill", got)
			}
			for _, number := range []uint32{x32SyscallBase, x32SyscallBase + target.socket} {
				if got := evaluate(filter, target.audit, number, unix.AF_UNIX); got != actionErrno|uint32(unix.ENOSYS) {
					t.Errorf("an x32 syscall got action %#x, want errno", got)
				}
			}
		})
	}
}

func TestTheSyscallFilterAllowsProcessSessions(t *testing.T) {
	target, err := architecture()
	if err != nil {
		t.Fatal(err)
	}

	filter, err := buildFilter(true)
	if err != nil {
		t.Fatal(err)
	}

	for _, number := range []uint32{uint32(unix.SYS_SETPGID), uint32(unix.SYS_SETSID)} {
		if got := evaluate(filter, target.audit, number, 0); got != actionAllow {
			t.Errorf("syscall %d: got action %#x, want allow", number, got)
		}
	}
}

const unixSocketsVariable = "IO_SANDBOX_UNIX_SOCKETS"

func TestAnInstalledFilterRefusesTheFamiliesItNames(t *testing.T) {
	if !insideChildProcess() {
		runAgainInChildProcess(t, unixSocketsVariable+"=1")
		return
	}

	allowUnixSockets := os.Getenv(unixSocketsVariable) != ""

	if err := applySeccomp(allowUnixSockets); err != nil {
		t.Fatalf("could not install the filter: %v", err)
	}

	for _, family := range []int{unix.AF_PACKET, unix.AF_NETLINK} {
		fd, err := unix.Socket(family, unix.SOCK_RAW, 0)
		if err == nil {
			_ = unix.Close(fd)
			t.Errorf("family %d was allowed", family)
			continue
		}
		if !errors.Is(err, unix.EAFNOSUPPORT) {
			t.Errorf("family %d: got %v, want the filter's refusal", family, err)
		}
	}

	for _, family := range []int{unix.AF_INET, unix.AF_UNIX} {
		fd, err := unix.Socket(family, unix.SOCK_DGRAM, 0)
		if err != nil {
			t.Errorf("family %d: got %v, want a socket the sandbox allows", family, err)
			continue
		}
		_ = unix.Close(fd)
	}
}

func TestAFilterWithoutUnixSocketIsolationRefusesThemOutright(t *testing.T) {
	if !insideChildProcess() {
		runAgainInChildProcess(t)
		return
	}

	if err := applySeccomp(false); err != nil {
		t.Fatalf("could not install the filter: %v", err)
	}

	fd, err := unix.Socket(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	if err == nil {
		_ = unix.Close(fd)
		t.Fatal("a Unix socket was allowed where landlock could not confine it")
	}
	if !errors.Is(err, unix.EAFNOSUPPORT) {
		t.Errorf("got %v, want the filter's refusal", err)
	}
}

func TestTheFilterLeavesNoNewPrivilegesBehindIt(t *testing.T) {
	if !insideChildProcess() {
		runAgainInChildProcess(t)
		return
	}

	if err := applySeccomp(true); err != nil {
		t.Fatalf("could not install the filter: %v", err)
	}

	set, err := unix.PrctlRetInt(unix.PR_GET_NO_NEW_PRIVS, 0, 0, 0, 0)
	if err != nil {
		t.Fatalf("could not read no_new_privs: %v", err)
	}
	if set != 1 {
		t.Error("a confined command could still gain privileges through a setuid binary")
	}
}

func evaluate(filter []unix.SockFilter, arch uint32, number uint32, argument uint32) uint32 {
	var accumulator uint32

	for at := 0; at < len(filter); {
		instruction := filter[at]

		switch instruction.Code {
		case unix.BPF_LD | unix.BPF_W | unix.BPF_ABS:
			switch instruction.K {
			case offsetArch:
				accumulator = arch
			case offsetNr:
				accumulator = number
			case offsetArgZero:
				accumulator = argument
			}
			at++
		case unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K:
			if accumulator == instruction.K {
				at += int(instruction.Jt) + 1
			} else {
				at += int(instruction.Jf) + 1
			}
		case unix.BPF_JMP | unix.BPF_JGE | unix.BPF_K:
			if accumulator >= instruction.K {
				at += int(instruction.Jt) + 1
			} else {
				at += int(instruction.Jf) + 1
			}
		case unix.BPF_RET | unix.BPF_K:
			return instruction.K
		default:
			panic("unknown filter instruction")
		}
	}

	panic("filter did not return")
}

func TestAnInstalledFilterRefusesTheX32ABI(t *testing.T) {
	if !insideChildProcess() {
		runAgainInChildProcess(t)
		return
	}

	if err := applySeccomp(false); err != nil {
		t.Fatalf("could not install the filter: %v", err)
	}

	fd, _, errno := unix.Syscall(
		uintptr(unix.SYS_SOCKET)+x32SyscallBase,
		uintptr(unix.AF_UNIX),
		uintptr(unix.SOCK_STREAM),
		0,
	)
	if errno == 0 {
		_ = unix.Close(int(fd))
	}
	if errno != unix.ENOSYS {
		t.Errorf("an x32 syscall got fd %d and %v, want the filter's refusal", fd, errno)
	}
}
