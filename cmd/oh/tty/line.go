package tty

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const linePollInterval = 100 * time.Millisecond

func ReadLine(ctx context.Context, input *os.File) (string, error) {
	var line strings.Builder
	fileDescriptor := int32(input.Fd()) //nolint:gosec // Unix file descriptors fit PollFd
	pollDescriptors := []unix.PollFd{{Fd: fileDescriptor, Events: unix.POLLIN}}

	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}

		ready, err := unix.Poll(pollDescriptors, int(linePollInterval.Milliseconds()))
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if err != nil {
			return "", err
		}
		if ready == 0 {
			continue
		}

		var character [1]byte
		read, err := input.Read(character[:])
		if read == 1 {
			if character[0] == '\n' {
				return strings.TrimSuffix(line.String(), "\r"), nil
			}
			line.WriteByte(character[0])
		}
		if errors.Is(err, io.EOF) {
			return line.String(), err
		}
		if err != nil {
			return "", err
		}
	}
}
