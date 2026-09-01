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
	"strconv"
	"strings"
	"time"

	"crdx.org/io/internal/transient"
)

const bodyLimit = 64 * 1024

type Client struct {
	http     *http.Client
	observer Observer
}

func New(timeout time.Duration) *Client {
	return &Client{http: &http.Client{Timeout: timeout}}
}

func (self *Client) Observe(observer Observer) {
	self.observer = observer
}

func (self *Client) Stream(
	ctx context.Context, address string, body any, header http.Header,
) (io.ReadCloser, http.Header, error) {
	return self.postJSON(ctx, address, body, header)
}

func (self *Client) JSON(ctx context.Context, address string, body any, target any) error {
	header := http.Header{}
	header.Set("Accept", "application/json")

	responseBody, _, err := self.postJSON(ctx, address, body, header)
	if err != nil {
		return err
	}
	defer func() { _ = responseBody.Close() }()

	if err := json.NewDecoder(responseBody).Decode(target); err != nil {
		return fmt.Errorf("parse the response: %w", err)
	}

	return nil
}

func (self *Client) Get(ctx context.Context, address string, header http.Header, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return err
	}

	if header != nil {
		request.Header = header.Clone()
	}

	request.Header.Set("Accept", "application/json")

	body, _, err := self.do(request, nil)
	if err != nil {
		return err
	}
	defer func() { _ = body.Close() }()

	if err := json.NewDecoder(body).Decode(target); err != nil {
		return fmt.Errorf("parse the response: %w", err)
	}

	return nil
}

func (self *Client) Form(ctx context.Context, address string, form url.Values, target any) error {
	encodedBody := form.Encode()
	request, err := http.NewRequestWithContext(
		ctx, http.MethodPost, address, strings.NewReader(encodedBody),
	)
	if err != nil {
		return err
	}

	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	body, _, err := self.do(request, []byte(encodedBody))
	if err != nil {
		return err
	}
	defer func() { _ = body.Close() }()

	if err := json.NewDecoder(body).Decode(target); err != nil {
		return fmt.Errorf("parse the response: %w", err)
	}

	return nil
}

func (self *Client) postJSON(
	ctx context.Context, address string, body any, header http.Header,
) (io.ReadCloser, http.Header, error) {
	encodedBody, err := json.Marshal(body)
	if err != nil {
		return nil, nil, fmt.Errorf("encode request: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, address, bytes.NewReader(encodedBody))
	if err != nil {
		return nil, nil, err
	}

	if header != nil {
		request.Header = header.Clone()
	}

	request.Header.Set("Content-Type", "application/json")

	return self.do(request, encodedBody)
}

func (self *Client) do(request *http.Request, requestBody []byte) (io.ReadCloser, http.Header, error) {
	var exchange ExchangeObserver
	if self.observer != nil {
		exchange = self.observer.Start(Request{
			StartedAt: time.Now(),
			Method:    request.Method,
			URL:       request.URL.String(),
			Protocol:  request.Proto,
			Header:    request.Header.Clone(),
			Body:      bytes.Clone(requestBody),
		})
	}

	response, err := self.http.Do(request)
	if err != nil {
		err = transient.Wrap(err)
		if exchange != nil {
			exchange.Finish(time.Now(), err, false)
		}

		return nil, nil, err
	}

	if exchange != nil {
		exchange.Response(Response{
			ReceivedAt: time.Now(),
			Protocol:   response.Proto,
			Status:     response.Status,
			Code:       response.StatusCode,
			Header:     response.Header.Clone(),
		})
		response.Body = &observedBody{
			ReadCloser: response.Body,
			observer:   exchange,
		}
	}

	if response.StatusCode != http.StatusOK {
		defer func() { _ = response.Body.Close() }()

		return nil, nil, refusal(response)
	}

	return response.Body, response.Header, nil
}

type observedBody struct {
	io.ReadCloser

	observer   ExchangeObserver
	isFinished bool
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
	if !self.isFinished {
		self.finish(err, true)
	}
	return err
}

func (self *observedBody) finish(err error, isIncomplete bool) {
	if self.isFinished {
		return
	}
	self.isFinished = true
	if errors.Is(err, io.EOF) {
		err = nil
	}
	self.observer.Finish(time.Now(), err, isIncomplete)
}

const statusOverloaded = 529

var retriableStatuses = map[int]bool{
	http.StatusTooManyRequests:     true,
	http.StatusInternalServerError: true,
	http.StatusBadGateway:          true,
	http.StatusServiceUnavailable:  true,
	http.StatusGatewayTimeout:      true,
	http.StatusInsufficientStorage: true,
	statusOverloaded:               true,
}

type StatusError struct {
	Status  int
	Code    string
	Message string
	Body    string
	Wait    time.Duration
}

func (self *StatusError) Error() string {
	if self.Message != "" {
		return self.Message
	}

	if body := strings.TrimSpace(self.Body); body != "" {
		return fmt.Sprintf("request failed with status %d: %s", self.Status, body)
	}

	return fmt.Sprintf("request failed with status %d", self.Status)
}

func IsRejected(err error) bool {
	var refusedRequest *StatusError

	return errors.As(err, &refusedRequest) && refusedRequest.Status >= 400 && refusedRequest.Status < 500
}

func (self *StatusError) Retriable() bool {
	return retriableStatuses[self.Status]
}

func (self *StatusError) RetryAfter() time.Duration {
	return self.Wait
}

func refusal(response *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(response.Body, bodyLimit))

	refusedRequest := &StatusError{
		Status: response.StatusCode,
		Body:   string(body),
		Wait:   retryAfter(response.Header.Get("Retry-After")),
	}

	var payload struct {
		Error struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`

		Detail string `json:"detail"`
	}

	if json.Unmarshal(body, &payload) != nil {
		return refusedRequest
	}

	refusedRequest.Code = payload.Error.Code

	for _, sentence := range []string{payload.Error.Message, payload.Detail} {
		if sentence != "" {
			refusedRequest.Message = sentence

			break
		}
	}

	return refusedRequest
}

func retryAfter(header string) time.Duration {
	header = strings.TrimSpace(header)
	if header == "" {
		return 0
	}

	if seconds, err := strconv.Atoi(header); err == nil {
		return max(time.Duration(seconds)*time.Second, 0)
	}

	if date, err := http.ParseTime(header); err == nil {
		return max(time.Until(date), 0)
	}

	return 0
}
