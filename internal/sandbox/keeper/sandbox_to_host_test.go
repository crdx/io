package keeper

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
)

func TestHostLoopbackDialUsesALiteralLoopbackAddress(t *testing.T) {
	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(t.Context(), "tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("IPv4 loopback is unavailable: %v", err)
	}
	defer func() { _ = listener.Close() }()

	accepted := make(chan net.Conn, 1)
	go func() {
		connection, _ := listener.Accept()
		accepted <- connection
	}()

	port := addressPort(t, listener.Addr())
	connection, err := dialLoopback(t.Context(), port)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()

	hostConnection := <-accepted
	if hostConnection == nil {
		t.Fatal("the host listener did not accept the connection")
	}
	_ = hostConnection.Close()

	host, _, err := net.SplitHostPort(connection.RemoteAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	if address := net.ParseIP(host); address == nil || !address.IsLoopback() {
		t.Errorf("dial reached non-loopback address %s", connection.RemoteAddr())
	}
}

func addressPort(t *testing.T, address net.Addr) uint16 {
	t.Helper()

	_, portText, err := net.SplitHostPort(address.String())
	if err != nil {
		t.Fatal(err)
	}
	var port uint16
	if _, err := fmt.Sscan(portText, &port); err != nil {
		t.Fatal(err)
	}
	return port
}

func TestAPortCannotCarryTrafficInBothDirections(t *testing.T) {
	port := freeLoopbackPort(t)
	keeperProcess := &Keeper{hostToSandbox: map[uint16]*bridge{port: {}}}

	err := keeperProcess.OpenSandboxToHost(t.Context(), port)
	if err == nil || !strings.Contains(err.Error(), "host to the sandbox") {
		t.Errorf("got %v, want a directional conflict", err)
	}

	keeperProcess = &Keeper{sandboxToHost: map[uint16]*bridge{port: {}}}
	err = keeperProcess.OpenHostToSandbox(t.Context(), testExposeHost, port)
	if err == nil || !strings.Contains(err.Error(), "sandbox to the host") {
		t.Errorf("got %v, want a directional conflict", err)
	}
}

func TestABridgeBoundsItsConnections(t *testing.T) {
	joined := &bridge{slots: make(chan struct{}, maxBridgeConnections)}
	for range maxBridgeConnections {
		if !joined.acquire() {
			t.Fatal("a connection below the limit was refused")
		}
	}
	if joined.acquire() {
		t.Fatal("a connection beyond the limit was accepted")
	}
	joined.release()
	if !joined.acquire() {
		t.Fatal("a released connection slot was not reusable")
	}
}

func TestCancellingAHostLoopbackDialStopsIt(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := dialLoopback(ctx, 1)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("got %v, want cancellation", err)
	}
}
