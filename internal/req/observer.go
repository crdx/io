package req

import (
	"net/http"
	"time"
)

// Request is an immutable snapshot of one logical HTTP request.
type Request struct {
	Started  time.Time
	Method   string
	URL      string
	Protocol string
	Header   http.Header
	Body     []byte
}

// Response is an immutable snapshot of one logical HTTP response.
type Response struct {
	Received time.Time
	Protocol string
	Status   string
	Code     int
	Header   http.Header
}

// Observer receives logical HTTP exchanges without affecting request control flow.
type Observer interface {
	Start(Request) ExchangeObserver
}

// ExchangeObserver receives one response body and its terminal state.
type ExchangeObserver interface {
	Response(Response)
	Body([]byte)
	Finish(time.Time, error, bool)
}
