package req

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const bodyLimit = 64 * 1024

// Client is an endpoint spoken to under a timeout, since the default client waits forever.
type Client struct {
	http     *http.Client // the timed HTTP client
	observer Observer     // optional logical exchange observer
}

// New builds a client that gives up on a request after the given wait.
func New(timeout time.Duration) *Client {
	return &Client{http: &http.Client{Timeout: timeout}}
}

// Observe attaches a logical exchange observer.
func (self *Client) Observe(observer Observer) {
	self.observer = observer
}

// Stream posts a JSON body and hands back the response, which is the caller's to close.
func (self *Client) Stream(ctx context.Context, address string, body any, header http.Header) (io.ReadCloser, error) {
	encodedBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, address, bytes.NewReader(encodedBody))
	if err != nil {
		return nil, err
	}

	if header != nil {
		request.Header = header
	}

	request.Header.Set("Content-Type", "application/json")

	return self.do(request, encodedBody)
}

// Form posts a form and reads the JSON answer into target.
func (self *Client) Form(ctx context.Context, address string, form url.Values, target any) error {
	encodedBody := form.Encode()
	request, err := http.NewRequestWithContext(
		ctx, http.MethodPost, address, strings.NewReader(encodedBody),
	)
	if err != nil {
		return err
	}

	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	body, err := self.do(request, []byte(encodedBody))
	if err != nil {
		return err
	}
	defer func() { _ = body.Close() }()

	if err := json.NewDecoder(body).Decode(target); err != nil {
		return fmt.Errorf("parse the response: %w", err)
	}

	return nil
}

func (self *Client) do(request *http.Request, requestBody []byte) (io.ReadCloser, error) {
	var exchange ExchangeObserver
	if self.observer != nil {
		exchange = self.observer.Start(Request{
			Started:  time.Now(),
			Method:   request.Method,
			URL:      request.URL.String(),
			Protocol: request.Proto,
			Header:   request.Header.Clone(),
			Body:     bytes.Clone(requestBody),
		})
	}

	response, err := self.http.Do(request)
	if err != nil {
		if exchange != nil {
			exchange.Finish(time.Now(), err, false)
		}
		return nil, err
	}

	if exchange != nil {
		exchange.Response(Response{
			Received: time.Now(),
			Protocol: response.Proto,
			Status:   response.Status,
			Code:     response.StatusCode,
			Header:   response.Header.Clone(),
		})
		response.Body = &observedBody{
			ReadCloser: response.Body,
			observer:   exchange,
		}
	}

	if response.StatusCode != http.StatusOK {
		defer func() { _ = response.Body.Close() }()

		return nil, refusal(response)
	}

	return response.Body, nil
}

type observedBody struct {
	io.ReadCloser

	observer ExchangeObserver
	finished bool
}

func (self *observedBody) Read(buffer []byte) (int, error) {
	count, err := self.ReadCloser.Read(buffer)
	if count > 0 {
		self.observer.Body(bytes.Clone(buffer[:count]))
	}
	if err != nil {
		self.finish(err, false)
	}
	return count, err
}

func (self *observedBody) Close() error {
	err := self.ReadCloser.Close()
	if !self.finished {
		self.finish(err, true)
	}
	return err
}

func (self *observedBody) finish(err error, incomplete bool) {
	if self.finished {
		return
	}
	self.finished = true
	if errors.Is(err, io.EOF) {
		err = nil
	}
	self.observer.Finish(time.Now(), err, incomplete)
}

func refusal(response *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(response.Body, bodyLimit))

	var payload struct {
		Error struct {
			Message string `json:"message"` // what went wrong
		} `json:"error"` // the endpoint error
	}

	if json.Unmarshal(body, &payload) != nil || payload.Error.Message == "" {
		return fmt.Errorf("request failed with status %d: %s", response.StatusCode, body)
	}

	return errors.New(payload.Error.Message)
}
