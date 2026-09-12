package sandbox

import "golang.org/x/sys/unix"

func IsIPv6Reachable() bool {
	descriptor, err := unix.Socket(unix.AF_INET6, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return false
	}

	_ = unix.Close(descriptor)

	return true
}
