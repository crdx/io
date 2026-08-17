package sandbox

import (
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	offsetNr      = 0
	offsetArch    = 4
	offsetArgZero = 16 // the low half of args[0] on a little-endian machine
)

const (
	actionKillProcess = 0x80000000
	actionErrno       = 0x00050000
	actionAllow       = 0x7fff0000
)

const seccompSetModeFilter = 1

func blockedFamilies(unixSockets bool) []uint32 {
	families := []uint32{unix.AF_PACKET, unix.AF_NETLINK}
	if !unixSockets {
		families = append(families, unix.AF_UNIX)
	}

	return families
}

func applySeccomp(unixSockets bool, background bool) error {
	filter, err := buildFilter(unixSockets, background)
	if err != nil {
		return err
	}

	program := unix.SockFprog{
		Len:    uint16(len(filter)), //nolint:gosec // the filter is a fixed handful of instructions
		Filter: &filter[0],
	}

	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("could not set no_new_privs: %w", err)
	}

	_, _, errno := unix.Syscall(
		unix.SYS_SECCOMP,
		uintptr(seccompSetModeFilter),
		0,
		uintptr(unsafe.Pointer(&program)), //nolint:gosec // the syscall takes the program by address
	)

	if errno != 0 {
		return fmt.Errorf("could not install the syscall filter: %w", errno)
	}

	return nil
}

func buildFilter(unixSockets bool, background bool) ([]unix.SockFilter, error) {
	target, err := architecture()
	if err != nil {
		return nil, err
	}

	blockedSocketFamilies := blockedFamilies(unixSockets)
	filter := []unix.SockFilter{
		load(offsetArch),
		jumpIfEqual(target.audit, 1, 0),
		ret(actionKillProcess),

		load(offsetNr),
	}

	if !background {
		filter = append(filter,
			jumpIfEqual(uint32(unix.SYS_SETPGID), 0, 1),
			ret(actionErrno|uint32(unix.EPERM)),
			jumpIfEqual(uint32(unix.SYS_SETSID), 0, 1),
			ret(actionErrno|uint32(unix.EPERM)),
		)
	}

	filter = append(filter,
		jumpIfEqual(target.socket, 0, uint8(len(blockedSocketFamilies)+1)), //nolint:gosec // a fixed handful
		load(offsetArgZero),
	)

	for index, family := range blockedSocketFamilies {
		//nolint:gosec // a fixed handful
		filter = append(filter, jumpIfEqual(family, uint8(len(blockedSocketFamilies)-index), 0))
	}

	filter = append(filter,
		ret(actionAllow),
		ret(actionErrno|uint32(unix.EAFNOSUPPORT)),
	)

	return filter, nil
}

type target struct {
	audit  uint32 // the AUDIT_ARCH value the filter must match
	socket uint32 // the socket(2) number
}

func architecture() (target, error) {
	switch runtime.GOARCH {
	case "amd64":
		return target{audit: unix.AUDIT_ARCH_X86_64, socket: 41}, nil
	case "arm64":
		return target{audit: unix.AUDIT_ARCH_AARCH64, socket: 198}, nil
	default:
		return target{}, fmt.Errorf("no syscall filter is defined for %s", runtime.GOARCH)
	}
}

func load(offset uint32) unix.SockFilter {
	return unix.SockFilter{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: offset}
}

func jumpIfEqual(value uint32, whenTrue uint8, whenFalse uint8) unix.SockFilter {
	return unix.SockFilter{
		Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K,
		Jt:   whenTrue,
		Jf:   whenFalse,
		K:    value,
	}
}

func ret(action uint32) unix.SockFilter {
	return unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: action}
}
