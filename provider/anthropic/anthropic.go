package anthropic

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/internal/req"
	"crdx.org/io/tool"
)

const (
	Endpoint = "https://api.anthropic.com/v1/messages"

	Version = "2023-06-01"

	Beta = "claude-code-20250219,oauth-2025-04-20,fine-grained-tool-streaming-2025-05-14"

	UserAgent = "claude-cli/2.1.75"

	Identity = "You are Claude Code, Anthropic's official CLI for Claude."
)

var Efforts = []string{"low", "medium", "high", "xhigh", "max"}

const turnTimeout = 60 * time.Minute

type Client struct {
	URL             string
	Model           string
	Effort          string
	MaxOutputTokens int

	tokens       TokenSource
	instructions string
	tools        []functionTool
	toolNames    []string
	history      []json.RawMessage
	requests     *req.Client
	observer     req.Observer
}

func New(tokens TokenSource, model string, effort string, maxOutputTokens int) (*Client, error) {
	client := &Client{
		URL:             Endpoint,
		Model:           model,
		Effort:          effort,
		MaxOutputTokens: maxOutputTokens,
		tokens:          tokens,
		requests:        req.New(turnTimeout),
	}

	if err := client.settled(); err != nil {
		return nil, err
	}

	return client, nil
}

func Auth(model string, effort string, maxOutputTokens int) (*Client, error) {
	return New(StoredCredentials(), model, effort, maxOutputTokens)
}

func (self *Client) ObserveHTTP(observer req.Observer) {
	self.observer = observer
	self.requests.Observe(observer)
	if source, ok := self.tokens.(observedTokenSource); ok {
		source.observeHTTP(observer)
	}
}

func (self *Client) Configure(instructions string, tools []tool.Definition) {
	self.instructions = instructions
	self.tools = describe(tools)

	self.toolNames = make([]string, len(tools))
	for i, offer := range tools {
		self.toolNames[i] = offer.Name
	}
}

func (self *Client) AddUserMessage(text string) {
	self.history = append(self.history, encodeItem(message{
		Role:    "user",
		Content: []json.RawMessage{encodeItem(textBlock{Type: "text", Text: text})},
	}))
}

func (self *Client) AddToolResults(results []agent.ToolCallResult) {
	blocks := make([]json.RawMessage, 0, len(results))

	for _, result := range results {
		blocks = append(blocks, encodeItem(toolResult{
			Type:      "tool_result",
			ToolUseID: result.ID,
			Content:   encodeToolOutput(result),
		}))
	}

	self.history = append(self.history, encodeItem(message{Role: "user", Content: blocks}))
}

func (self *Client) Dump() []json.RawMessage {
	return slices.Clone(self.history)
}

func (self *Client) Load(items []json.RawMessage) {
	self.history = slices.Clone(items)
}

func encodeItem(item any) json.RawMessage {
	encodedItem, _ := json.Marshal(item) //nolint:errchkjson // the wire items are plain structs

	return encodedItem
}

func (self *Client) Send(ctx context.Context, yield agent.Yield) (agent.Reply, error) {
	reply, err := self.post(ctx, yield)
	if err != nil {
		if said := reply.prose(); said != nil {
			self.history = append(self.history, said)
		}

		return agent.Reply{}, err
	}

	if answer := reply.message(); answer != nil {
		self.history = append(self.history, answer)
	}

	return agent.Reply{
		Calls: reply.calls(self.toolNames),
		Usage: reply.usage,
	}, nil
}

func (self *Client) settled() error {
	for _, setting := range []struct {
		name  string
		value string
	}{
		{"URL", self.URL},
		{"Model", self.Model},
		{"Effort", self.Effort},
	} {
		if setting.value == "" {
			return fmt.Errorf("anthropic: %s is empty", setting.name)
		}
	}

	if self.MaxOutputTokens <= 0 {
		return fmt.Errorf("anthropic: MaxOutputTokens is %d, and must be above zero", self.MaxOutputTokens)
	}

	if !slices.Contains(Efforts, self.Effort) {
		return fmt.Errorf(
			"anthropic: Effort is %q, and must be one of: %s",
			self.Effort, strings.Join(Efforts, ", "),
		)
	}

	return nil
}

func (self *Client) post(ctx context.Context, yield agent.Yield) (reply, error) {
	token, err := self.tokens.Token()
	if err != nil {
		return reply{}, err
	}

	stream, _, err := self.requests.Stream(ctx, self.URL, self.requestBody(), self.headers(token))
	if err != nil {
		return reply{}, err
	}
	defer func() { _ = stream.Close() }()

	return readReply(stream, yield)
}

func (self *Client) requestBody() request {
	return request{
		Model:           self.Model,
		MaxOutputTokens: self.MaxOutputTokens,
		Stream:          true,
		System:          self.system(),
		Tools:           self.tools,
		Thinking:        thinking{Type: "adaptive", Display: "summarized"},
		Output:          outputConfig{Effort: self.Effort},
		Messages:        cacheable(merged(self.history)),
	}
}

func (self *Client) system() []textBlock {
	blocks := []textBlock{{Type: "text", Text: Identity, Cache: ephemeral()}}

	if self.instructions != "" {
		blocks = append(blocks, textBlock{Type: "text", Text: self.instructions, Cache: ephemeral()})
	}

	return blocks
}

func (self *Client) headers(token string) http.Header {
	header := http.Header{}

	header.Set("Authorization", "Bearer "+token)
	header.Set("Anthropic-Version", Version)
	header.Set("Anthropic-Beta", Beta)
	header.Set("Anthropic-Dangerous-Direct-Browser-Access", "true")
	header.Set("Accept", "text/event-stream")
	header.Set("User-Agent", UserAgent)
	header.Set("X-App", "cli")

	return header
}

type request struct {
	Model           string            `json:"model"`
	MaxOutputTokens int               `json:"max_tokens"`
	Stream          bool              `json:"stream"`
	System          []textBlock       `json:"system,omitempty"`
	Tools           []functionTool    `json:"tools,omitempty"`
	Messages        []json.RawMessage `json:"messages"`
	Thinking        thinking          `json:"thinking"`
	Output          outputConfig      `json:"output_config"`
}

type thinking struct {
	Type    string `json:"type"`
	Display string `json:"display"`
}

type outputConfig struct {
	Effort string `json:"effort"`
}

type message struct {
	Role    string            `json:"role"`
	Content []json.RawMessage `json:"content"`
}

type textBlock struct {
	Type  string        `json:"type"`
	Text  string        `json:"text"`
	Cache *cacheControl `json:"cache_control,omitempty"`
}

type toolResult struct {
	Type      string `json:"type"`
	ToolUseID string `json:"tool_use_id"`
	Content   any    `json:"content"`
}

type imageBlock struct {
	Type   string      `json:"type"`
	Source imageSource `json:"source"`
}

type imageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

const attachmentNotice = "(see attached image)"

func encodeToolOutput(result agent.ToolCallResult) any {
	if result.Image.MediaType == "" || len(result.Image.Data) == 0 {
		return result.Output
	}

	text := result.Output
	if text == "" {
		text = attachmentNotice
	}

	return []any{
		textBlock{Type: "text", Text: text},
		imageBlock{
			Type: "image",
			Source: imageSource{
				Type:      "base64",
				MediaType: result.Image.MediaType,
				Data:      base64.StdEncoding.EncodeToString(result.Image.Data),
			},
		},
	}
}
