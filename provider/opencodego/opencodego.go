package opencodego

import (
	"fmt"
	"net/http"
	"time"

	"crdx.org/io/internal/req"
	"crdx.org/io/tool"
	"crdx.org/io/wire/openai/chatcompletions"
)

const asideTimeout = 30 * time.Second

type Client struct {
	*chatcompletions.Client

	UsageURL string
	Token    string

	observer req.Observer
}

func New(
	url string,
	token string,
	model string,
	effort string,
	maxOutputTokens int,
) (*Client, error) {
	for _, setting := range []struct {
		name  string
		value string
	}{
		{"URL", url},
		{"Model", model},
		{"Token", token},
	} {
		if setting.value == "" {
			return nil, fmt.Errorf("chat: %s is empty", setting.name)
		}
	}

	header := http.Header{}
	header.Set("Authorization", "Bearer "+token)
	conversation, err := chatcompletions.New(url, header, model, effort, maxOutputTokens)
	if err != nil {
		return nil, err
	}

	return &Client{Client: conversation, Token: token}, nil
}

func (self *Client) ObserveHTTP(observer req.Observer) {
	self.observer = observer
	self.Client.ObserveHTTP(observer)
}

func (self *Client) observedRequests() *req.Client {
	client := req.New(asideTimeout)
	if self.observer != nil {
		client.Observe(self.observer)
	}

	return client
}

func (self *Client) headers() http.Header {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+self.Token)
	return header
}

func ToolsSize(tools []tool.Tool) int {
	return chatcompletions.ToolsSize(tools)
}
