package codex

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"crdx.org/io/internal/req"
)

// An endpoint that accepts the connection and then says nothing must not hold a refresh open
// forever, since Token holds its lock across the exchange and every request behind it waits.
func TestPostFormGivesUpOnAStalledEndpoint(t *testing.T) {
	stalled := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(
		func(http.ResponseWriter, *http.Request) { <-stalled }))

	t.Cleanup(func() { close(stalled); server.Close() })

	TokenURL = server.URL
	t.Cleanup(func() { TokenURL = "" })

	authClient = req.New(50 * time.Millisecond)
	t.Cleanup(func() { authClient = req.New(authTimeout) })

	answered := make(chan error, 1)
	go func() {
		_, err := refreshToken("refresh-me")
		answered <- err
	}()

	select {
	case err := <-answered:
		if err == nil {
			t.Fatal("expected the stalled endpoint to be given up on")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected the exchange to give up, but it is still waiting")
	}
}
