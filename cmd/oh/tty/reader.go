package tty

import (
	"errors"
	"io"
	"os"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const readPollInterval = 100 * time.Millisecond

type Reader struct {
	input    *os.File
	stopping chan struct{}
	once     sync.Once
}

func NewReader(input *os.File) *Reader {
	return &Reader{input: input, stopping: make(chan struct{})}
}

func (self *Reader) Stop() {
	self.once.Do(func() { close(self.stopping) })
}

func (self *Reader) Stopping() <-chan struct{} {
	return self.stopping
}

func (self *Reader) Read(buffer []byte) (int, error) {
	fileDescriptor := int32(self.input.Fd()) //nolint:gosec // Unix file descriptors fit PollFd
	pollDescriptors := []unix.PollFd{{Fd: fileDescriptor, Events: unix.POLLIN}}

	for {
		select {
		case <-self.stopping:
			return 0, io.EOF
		default:
		}

		ready, err := unix.Poll(pollDescriptors, int(readPollInterval.Milliseconds()))
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if err != nil {
			return 0, err
		}
		if ready == 0 {
			continue
		}

		return self.input.Read(buffer)
	}
}
