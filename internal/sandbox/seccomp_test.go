package sandbox

import (
	"testing"

	"golang.org/x/sys/unix"
)

func TestTheSocketFilterAllowsOnlyNamespacedNetworking(t *testing.T) {
	target, err := architecture()
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name        string
		unixSockets bool
		unixAction  uint32
	}{
		{name: "before Unix socket isolation", unixAction: actionErrno | uint32(unix.EAFNOSUPPORT)},
		{name: "with Unix socket isolation", unixSockets: true, unixAction: actionAllow},
	} {
		t.Run(test.name, func(t *testing.T) {
			filter, err := buildFilter(test.unixSockets, false)
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
		})
	}
}

func TestTheSyscallFilterAllowsBackgroundingOnlyWhereAsked(t *testing.T) {
	target, err := architecture()
	if err != nil {
		t.Fatal(err)
	}

	for _, background := range []bool{false, true} {
		filter, err := buildFilter(true, background)
		if err != nil {
			t.Fatal(err)
		}

		want := actionErrno | uint32(unix.EPERM)
		if background {
			want = actionAllow
		}

		for _, number := range []uint32{uint32(unix.SYS_SETPGID), uint32(unix.SYS_SETSID)} {
			if got := evaluate(filter, target.audit, number, 0); got != want {
				t.Errorf("background %t, syscall %d: got action %#x, want %#x", background, number, got, want)
			}
		}
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
		case unix.BPF_RET | unix.BPF_K:
			return instruction.K
		default:
			panic("unknown filter instruction")
		}
	}

	panic("filter did not return")
}
