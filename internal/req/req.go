package req

import (
	"bytes"
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
	http *http.Client
}

// New builds a client that gives up on a request after the given wait.
func New(timeout time.Duration) *Client {
	return &Client{http: &http.Client{Timeout: timeout}}
}

// Stream posts a JSON body and hands back the response, which is the caller's to close.
func (self *Client) Stream(address string, body any, header http.Header) (io.ReadCloser, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}

	request, err := http.NewRequest(http.MethodPost, address, bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}

	if header != nil {
		request.Header = header
	}

	request.Header.Set("Content-Type", "application/json")

	return self.do(request)
}

// Form posts a form and reads the JSON answer into target.
func (self *Client) Form(address string, form url.Values, target any) error {
	request, err := http.NewRequest(
		http.MethodPost, address, strings.NewReader(form.Encode()),
	)
	if err != nil {
		return err
	}

	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	body, err := self.do(request)
	if err != nil {
		return err
	}
	defer func() { _ = body.Close() }()

	if err := json.NewDecoder(body).Decode(target); err != nil {
		return fmt.Errorf("parse the response: %w", err)
	}

	return nil
}

func (self *Client) do(request *http.Request) (io.ReadCloser, error) {
	response, err := self.http.Do(request)
	if err != nil {
		return nil, err
	}

	if response.StatusCode != http.StatusOK {
		defer func() { _ = response.Body.Close() }()

		return nil, refusal(response)
	}

	return response.Body, nil
}

func refusal(response *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(response.Body, bodyLimit))

	var payload struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if json.Unmarshal(body, &payload) != nil || payload.Error.Message == "" {
		return fmt.Errorf("request failed with status %d: %s", response.StatusCode, body)
	}

	return errors.New(payload.Error.Message)
}
