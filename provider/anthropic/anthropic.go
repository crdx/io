// Package anthropic is a provider speaking the Messages API, authorised by a Claude subscription
// rather than by an API key.
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

// reference/messages-create.md
// reference/models.md
const (
	Endpoint = "https://api.anthropic.com/v1/messages"

	// Version is the API revision every request declares.
	Version = "2023-06-01"

	// Beta is what the endpoint is asked to enable. A token authorised against a subscription is
	// only honoured when the request presents itself as Claude Code, which is what the first two
	// features here say. Interleaved thinking is not among them because a model taking adaptive
	// thinking already has it, and naming it there is redundant.
	Beta = "claude-code-20250219,oauth-2025-04-20,fine-grained-tool-streaming-2025-05-14"

	// UserAgent is who the endpoint is told is asking.
	UserAgent = "claude-cli/2.1.75"

	// Identity is what a subscription token requires the system prompt to open with, before
	// anything a caller has to say.
	Identity = "You are Claude Code, Anthropic's official CLI for Claude."
)

// Efforts are how hard the model may be asked to work, least to most. Effort shapes the rendered
// prompt, so changing it mid-conversation drops the cached prefix the turns before it built.
//
// reference/effort.md
var Efforts = []string{"low", "medium", "high", "xhigh", "max"}

const turnTimeout = 60 * time.Minute

// Client speaks the Messages API: one request per turn, answered as a stream of content blocks.
// What a client holds is what it sends: the model and the effort are asked of New, which refuses
// to build a client missing either, and nothing is substituted later.
//
// reference/streaming.md
type Client struct {
	URL             string
	Model           string
	Effort          string
	MaxOutputTokens int // the ceiling on thinking and answer together

	tokens       TokenSource
	instructions string
	tools        []functionTool
	toolNames    []string
	history      []json.RawMessage
	requests     *req.Client
	observer     req.Observer
}

// New builds a client asking the given model at the given effort, authorising every request with
// the given source. None of the three has a default: which model to ask, how hard, and how much it
// may write are the caller's to decide and this package's to carry out. The ceiling belongs to the
// model rather than to this package, and the listing Models returns reports it.
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

// Auth is a client on the credentials the login command stored.
func Auth(model string, effort string, maxOutputTokens int) (*Client, error) {
	return New(StoredCredentials(), model, effort, maxOutputTokens)
}

// ObserveHTTP attaches an observer to session requests and credential refreshes.
func (self *Client) ObserveHTTP(observer req.Observer) {
	self.observer = observer
	self.requests.Observe(observer)
	if source, ok := self.tokens.(observedTokenSource); ok {
		source.observeHTTP(observer)
	}
}

// Configure takes what every request in the session carries.
//
// reference/tool-use.md
func (self *Client) Configure(instructions string, tools []tool.Definition) {
	self.instructions = instructions
	self.tools = describe(tools)

	self.toolNames = make([]string, len(tools))
	for i, offer := range tools {
		self.toolNames[i] = offer.Name
	}
}

// AddUserMessage appends a message to the conversation.
func (self *Client) AddUserMessage(text string) {
	self.history = append(self.history, encodeItem(message{
		Role:    "user",
		Content: []json.RawMessage{encodeItem(textBlock{Type: "text", Text: text})},
	}))
}

// AddToolResults appends this turn's tool call results to the conversation. Every result of one
// round goes into a single message, which is the shape the endpoint expects them in.
//
// reference/tool-use.md
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

// Dump hands over the conversation so far, one message per entry, in the order the endpoint expects
// them back. New state is appended and earlier items are never replaced.
func (self *Client) Dump() []json.RawMessage {
	return slices.Clone(self.history)
}

// Load takes a conversation back, replacing whatever this client held.
func (self *Client) Load(items []json.RawMessage) {
	self.history = slices.Clone(items)
}

func encodeItem(item any) json.RawMessage {
	encodedItem, _ := json.Marshal(item) //nolint:errchkjson // every field has a safe encoder

	return encodedItem
}

// Send posts the conversation so far and reads the response. A turn that fails part-way keeps what
// the model said and thought before it failed, because that much was already reported to whoever
// was watching, and a conversation missing it disagrees with what they saw.
//
// reference/streaming.md
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

	stream, err := self.requests.Stream(ctx, self.URL, self.requestBody(), self.headers(token))
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
	MaxOutputTokens int               `json:"max_tokens"` // what the model may write, which this API calls max_tokens
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
