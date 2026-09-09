package keeper

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"crdx.org/io/internal/sandbox/loopback"

	"golang.org/x/sys/unix"
)

const forwardDialTimeout = 5 * time.Second

func encodeForwardPorts(ports []uint16) string {
	portTexts := make([]string, len(ports))
	for i, port := range ports {
		portTexts[i] = strconv.FormatUint(uint64(port), 10)
	}
	return strings.Join(portTexts, ",")
}

func decodeForwardPorts(portList string) ([]uint16, error) {
	if portList == "" {
		return nil, nil
	}

	var ports []uint16
	seen := make(map[uint16]struct{})
	for portText := range strings.SplitSeq(portList, ",") {
		portNumber, err := strconv.ParseUint(portText, 10, 16)
		if err != nil || portNumber == 0 {
			return nil, fmt.Errorf("invalid host loopback port %q", portText)
		}
		port := uint16(portNumber)
		if _, exists := seen[port]; exists {
			return nil, fmt.Errorf("host loopback port %d is repeated", port)
		}
		seen[port] = struct{}{}
		ports = append(ports, port)
	}
	return ports, nil
}

func openForwardListeners(ports []uint16) ([]*os.File, error) {
	if len(ports) == 0 {
		return nil, nil
	}
	if err := loopback.Up(); err != nil {
		return nil, err
	}

	files := make([]*os.File, 0, len(ports))
	var listenConfig net.ListenConfig
	for _, port := range ports {
		listener, err := listenConfig.Listen(context.Background(), "tcp", ":"+strconv.Itoa(int(port)))
		if err != nil {
			closeFiles(files)
			return nil, fmt.Errorf("could not forward host loopback port %d: %w", port, err)
		}
		tcp, ok := listener.(*net.TCPListener)
		if !ok {
			_ = listener.Close()
			closeFiles(files)
			return nil, errors.New("loopback listener is not TCP")
		}
		file, err := tcp.File()
		_ = listener.Close()
		if err != nil {
			closeFiles(files)
			return nil, fmt.Errorf("could not pass host loopback port %d: %w", port, err)
		}
		files = append(files, file)
	}
	return files, nil
}

func closeFiles(files []*os.File) {
	for _, file := range files {
		_ = file.Close()
	}
}

func newForwardBridge(ctx context.Context, file *os.File, port uint16) (*bridge, error) {
	listener, err := net.FileListener(file)
	if err != nil {
		return nil, err
	}
	return newBridge(ctx, listener, func(ctx context.Context) (net.Conn, error) {
		return dialLoopback(ctx, port)
	}), nil
}

func dialLoopback(ctx context.Context, port uint16) (net.Conn, error) {
	writtenPort := strconv.Itoa(int(port))
	var failures []error
	for _, target := range []struct {
		network string
		host    string
	}{
		{network: "tcp4", host: "127.0.0.1"},
		{network: "tcp6", host: "::1"},
	} {
		dialer := net.Dialer{Timeout: forwardDialTimeout}
		connection, err := dialer.DialContext(
			ctx, target.network, net.JoinHostPort(target.host, writtenPort),
		)
		if err == nil {
			return connection, nil
		}
		failures = append(failures, err)
	}
	return nil, errors.Join(failures...)
}

func parseFiles(control []byte) []*os.File {
	messages, err := unix.ParseSocketControlMessage(control)
	if err != nil {
		return nil
	}
	var files []*os.File
	for _, message := range messages {
		descriptors, err := unix.ParseUnixRights(&message)
		if err != nil {
			continue
		}
		for _, descriptor := range descriptors {
			unix.CloseOnExec(descriptor)
			files = append(files, os.NewFile(uintptr(descriptor), "loopback-forward"))
		}
	}
	return files
}
