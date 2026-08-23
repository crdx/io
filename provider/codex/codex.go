// Package codex is a provider speaking the Responses API, as the ChatGPT backend serves it rather
// than as the public API does.
package codex

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"slices"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/internal/req"
	"crdx.org/io/tool"
)

// https://platform.openai.com/docs/api-reference/responses/create
// https://platform.openai.com/docs/guides/reasoning
const (
	Endpoint = "https://chatgpt.com/backend-api/codex/responses"
	Summary  = "auto"

	Originator = "io"
)

// Efforts are how hard the model may be asked to work, least to most. Not every reasoning model
// takes every one of them.
//
// reference/responses-create.md
var Efforts = []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}

const turnTimeout = 60 * time.Minute

// Client speaks the Responses API: one request per turn, answered as a stream of events.
//
// https://platform.openai.com/docs/api-reference/responses/create
// https://platform.openai.com/docs/api-reference/responses-streaming
// https://platform.openai.com/docs/guides/conversation-state
type Client struct {
	URL    string
	Model  string
	Effort string

	tokens       TokenSource
	instructions string
	tools        []functionTool
	session      string
	history      []json.RawMessage
	requests     *req.Client
	observer     req.Observer // what watches every exchange, where one is attached
}

// New builds a client asking the given model at the given effort, authorising every request with
// the given source. Neither has a default: which model to ask and how hard are the caller's to
// decide and this package's to carry out, so a client is refused rather than built where either is
// missing.
//
// There is no ceiling to give it. How much a turn may write is one of the things this endpoint
// keeps to itself, so a caller holding a figure for it has nowhere here to put it and is not asked
// for one.
//
// https://platform.openai.com/docs/guides/prompt-caching
func New(tokens TokenSource, model string, effort string) (*Client, error) {
	client := &Client{
		URL:      Endpoint,
		Model:    model,
		Effort:   effort,
		tokens:   tokens,
		session:  newToken(),
		requests: req.New(turnTimeout),
	}

	if err := client.settled(); err != nil {
		return nil, err
	}

	return client, nil
}

// Auth is a client on the credentials the login command stored.
func Auth(model string, effort string) (*Client, error) {
	return New(StoredCredentials(), model, effort)
}

// Configure takes what every request in the session carries.
//
// https://platform.openai.com/docs/guides/function-calling
func (self *Client) Configure(instructions string, tools []tool.Definition) {
	self.instructions = instructions
	self.tools = describe(tools)
}

// ObserveHTTP attaches an observer to session requests and credential refreshes.
func (self *Client) ObserveHTTP(observer req.Observer) {
	self.observer = observer
	self.requests.Observe(observer)
	if source, ok := self.tokens.(observedTokenSource); ok {
		source.observeHTTP(observer)
	}
}

// AddUserMessage appends a message to the conversation.
//
// https://platform.openai.com/docs/guides/conversation-state
func (self *Client) AddUserMessage(text string) {
	self.history = append(self.history, encodeItem(userMessage{Role: "user", Content: text}))
}

// AddToolResults appends this turn's tool call results to the conversation.
//
// https://platform.openai.com/docs/guides/function-calling
func (self *Client) AddToolResults(results []agent.ToolCallResult) {
	for _, result := range results {
		self.history = append(self.history, encodeItem(toolOutput{
			Type:   "function_call_output",
			CallID: result.ID,
			Output: encodeToolOutput(result),
		}))
	}
}

// Dump hands over the conversation so far, one item per entry, in the order the endpoint expects
// them back. New state is appended and earlier items are never replaced.
//
// https://platform.openai.com/docs/guides/conversation-state
func (self *Client) Dump() []json.RawMessage {
	return slices.Clone(self.history)
}

// Load takes a conversation back, replacing whatever this client held.
func (self *Client) Load(items []json.RawMessage) {
	self.history = slices.Clone(items)
}

func encodeItem(item any) json.RawMessage {
	encodedItem, _ := json.Marshal(item) //nolint:errchkjson // a struct of strings cannot fail

	return encodedItem
}

// Send posts the conversation so far and reads the response.
//
// https://platform.openai.com/docs/api-reference/responses-streaming
func (self *Client) Send(ctx context.Context, yield agent.Yield) (agent.Reply, error) {
	turn, err := self.post(ctx, yield)
	if err != nil {
		self.history = append(self.history, turn.prose()...)

		return agent.Reply{}, err
	}

	self.history = append(self.history, turn.items...)

	return agent.Reply{Calls: turn.calls()}, nil
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
			return fmt.Errorf("codex: %s is empty", setting.name)
		}
	}

	return nil
}

func (self *Client) requestBody() request {
	return request{
		Model:             self.Model,
		Store:             false,
		Stream:            true,
		Input:             self.history,
		Reasoning:         reasoning{Effort: self.Effort, Summary: Summary},
		Include:           []string{"reasoning.encrypted_content"},
		PromptCacheKey:    self.session,
		ToolChoice:        "auto",
		ParallelToolCalls: true,
		Tools:             self.tools,
		Instructions:      self.instructions,
	}
}

func (self *Client) headers(token Token) http.Header {
	header := http.Header{}

	header.Set("Authorization", "Bearer "+token.Access)
	header.Set("Chatgpt-Account-Id", token.AccountID)
	header.Set("Originator", Originator)
	header.Set("Openai-Beta", "responses=experimental")
	header.Set("Accept", "text/event-stream")
	header.Set("Session_id", self.session)
	header.Set("User-Agent", fmt.Sprintf("io (%s; %s)", runtime.GOOS, runtime.GOARCH))

	return header
}

type request struct {
	Model             string         `json:"model"`
	Store             bool           `json:"store"`
	Tools             []functionTool `json:"tools"`
	Instructions      string         `json:"instructions,omitempty"`
	ParallelToolCalls bool           `json:"parallel_tool_calls"`

	Stream bool `json:"stream"`

	Input []json.RawMessage `json:"input"`

	Include []string `json:"include"`

	PromptCacheKey string `json:"prompt_cache_key"`

	ToolChoice string `json:"tool_choice"`

	Reasoning reasoning `json:"reasoning"`
}

type reasoning struct {
	Effort  string `json:"effort"`
	Summary string `json:"summary"`
}

type userMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type toolOutput struct {
	Type   string `json:"type"`
	CallID string `json:"call_id"`
	Output any    `json:"output"`
}

type inputContent struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

const imageDetail = "high"

func encodeToolOutput(result agent.ToolCallResult) any {
	if result.Image.MediaType == "" || len(result.Image.Data) == 0 {
		return result.Output
	}

	content := make([]inputContent, 0, 2)
	if result.Output != "" {
		content = append(content, inputContent{Type: "input_text", Text: result.Output})
	}

	content = append(content, inputContent{
		Type: "input_image",
		ImageURL: fmt.Sprintf(
			"data:%s;base64,%s",
			result.Image.MediaType,
			base64.StdEncoding.EncodeToString(result.Image.Data),
		),
		Detail: imageDetail,
	})

	return content
}

func newToken() string {
	buffer := make([]byte, 32)
	_, _ = rand.Read(buffer)

	return hex.EncodeToString(buffer)
}
