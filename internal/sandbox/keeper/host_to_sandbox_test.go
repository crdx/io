package keeper

import (
	"context"
	"errors"
	"io"
	"net"
	"slices"
	"strings"
	"testing"
)

const testExposeHost = "127.0.0.1"

func TestExposingAPortListensOnceAndSaysSoWhenItIsAlreadyOpen(t *testing.T) {
	keeperProcess := &Keeper{}
	port := freeLoopbackPort(t)

	if err := keeperProcess.OpenHostToSandbox(t.Context(), testExposeHost, port); err != nil {
		t.Fatalf("could not expose port %d: %v", port, err)
	}
	t.Cleanup(func() { _ = keeperProcess.CloseHostToSandbox(port) })

	if hostToSandboxPorts := keeperProcess.GetHostToSandboxPorts(); !slices.Equal(hostToSandboxPorts, []uint16{port}) {
		t.Errorf("got exposed ports %v, want [%d]", hostToSandboxPorts, port)
	}
	if err := keeperProcess.OpenHostToSandbox(t.Context(), testExposeHost, port); err == nil {
		t.Error("exposing a port twice was allowed")
	} else if !strings.Contains(err.Error(), "already exposed") {
		t.Errorf("got %v, want a refusal naming the exposed port", err)
	}
}

func TestHidingAPortClosesItAndSaysSoWhenItWasNotOpen(t *testing.T) {
	keeperProcess := &Keeper{}
	port := freeLoopbackPort(t)

	if err := keeperProcess.OpenHostToSandbox(t.Context(), testExposeHost, port); err != nil {
		t.Fatalf("could not expose port %d: %v", port, err)
	}
	if err := keeperProcess.CloseHostToSandbox(port); err != nil {
		t.Fatalf("could not hide port %d: %v", port, err)
	}
	if hostToSandboxPorts := keeperProcess.GetHostToSandboxPorts(); len(hostToSandboxPorts) != 0 {
		t.Errorf("got exposed ports %v, want none", hostToSandboxPorts)
	}
	if err := keeperProcess.CloseHostToSandbox(port); err == nil {
		t.Error("hiding a port that was not exposed was allowed")
	}

	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(t.Context(), "tcp", net.JoinHostPort(testExposeHost, portText(port)))
	if err != nil {
		t.Errorf("the exposed listener was not closed: %v", err)
		return
	}
	_ = listener.Close()
}

func TestExposingIsRefusedOnceTheKeeperIsClosed(t *testing.T) {
	keeperProcess := &Keeper{}
	keeperProcess.isClosed.Store(true)

	if err := keeperProcess.OpenHostToSandbox(t.Context(), testExposeHost, freeLoopbackPort(t)); !errors.Is(err, ErrClosed) {
		t.Errorf("got %v, want the keeper to be closed", err)
	}
}

func TestABridgeJoinsWhatItAcceptsToWhatItConnects(t *testing.T) {
	var listenConfig net.ListenConfig
	far, err := listenConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("loopback sockets are unavailable: %v", err)
	}
	defer func() { _ = far.Close() }()

	go func() {
		connection, err := far.Accept()
		if err != nil {
			return
		}
		said, _ := io.ReadAll(connection)
		_, _ = connection.Write([]byte("heard " + string(said)))
		_ = connection.Close()
	}()

	near, err := listenConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("loopback sockets are unavailable: %v", err)
	}

	joined := newBridge(t.Context(), near, func(ctx context.Context) (net.Conn, error) {
		var dialer net.Dialer
		return dialer.DialContext(ctx, "tcp", far.Addr().String())
	})
	defer func() { _ = joined.Close() }()

	var dialer net.Dialer
	connection, err := dialer.DialContext(t.Context(), "tcp", near.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Write([]byte("spoken")); err != nil {
		t.Fatal(err)
	}
	if tcp, isTCP := connection.(*net.TCPConn); isTCP {
		_ = tcp.CloseWrite()
	}
	answer, err := io.ReadAll(connection)
	_ = connection.Close()
	if err != nil {
		t.Fatal(err)
	}
	if string(answer) != "heard spoken" {
		t.Errorf("got %q, want the far side's answer", answer)
	}
}

func TestAConnectionIsTakenFromTheKeepersAnswerOrItsRefusal(t *testing.T) {
	if _, err := connectionFrom(arrival{}, false); !errors.Is(err, ErrClosed) {
		t.Errorf("got %v, want the keeper to be closed", err)
	}

	refusal := arrival{answer: reply{Kind: replyRefused, Failure: "connection refused"}}
	if _, err := connectionFrom(refusal, true); err == nil || err.Error() != "connection refused" {
		t.Errorf("got %v, want the keeper's own failure", err)
	}

	silence := arrival{answer: reply{Kind: replyHostToSandboxDialled}}
	if _, err := connectionFrom(silence, true); err == nil {
		t.Error("a reply carrying no connection was accepted")
	}
}

func freeLoopbackPort(t *testing.T) uint16 {
	t.Helper()

	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("loopback sockets are unavailable: %v", err)
	}
	port := addressPort(t, listener.Addr())
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}
