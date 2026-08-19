// Package codex is a provider speaking the Responses API.
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

// —————————————————————————————————————————————————————————————————————————————————————————————————
// mega:allow-file comment-lines
// —————————————————————————————————————————————————————————————————————————————————————————————————

// https://platform.openai.com/docs/api-reference/responses/create
// https://platform.openai.com/docs/guides/reasoning
const (
	Endpoint = "https://chatgpt.com/backend-api/codex/responses"
	Model    = "gpt-5.6-sol"
	Effort   = "high"
	Summary  = "auto" // defined as the "most detailed summarizer available for a model"

	Originator = "io" // who we?
)

const turnTimeout = 60 * time.Minute

// Client speaks the Responses API: one request per turn, answered as a stream of events.
//
// https://platform.openai.com/docs/api-reference/responses/create
// https://platform.openai.com/docs/api-reference/responses-streaming
// https://platform.openai.com/docs/guides/conversation-state
type Client struct {
	URL    string // the endpoint address
	Model  string // the model to ask
	Effort string // how hard the model should reason

	tokens       TokenSource       // where credentials come from
	instructions string            // what the model is told
	tools        []functionTool    // what the model may call
	session      string            // the conversation key
	history      []json.RawMessage // the provider conversation state
	requests     *req.Client       // the HTTP transport
}

// New builds a client that authorises every request with the given source.
//
// https://platform.openai.com/docs/guides/prompt-caching
func New(tokens TokenSource) *Client {
	return &Client{
		tokens:   tokens,
		session:  newToken(),
		requests: req.New(turnTimeout),
	}
}

// Auth is a client on the credentials the login command stored.
func Auth() *Client {
	return New(StoredCredentials())
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
	self.requests.Observe(observer)
	if source, ok := self.tokens.(observedTokenSource); ok {
		source.observeHTTP(observer)
	}
}

// AddUserMessage appends a prompt to the conversation.
//
// https://platform.openai.com/docs/guides/conversation-state
func (self *Client) AddUserMessage(prompt string) {
	self.history = append(self.history, encodeItem(userMessage{Role: "user", Content: prompt}))
}

// AddToolResults appends this turn's tool call results to the conversation.
//
// https://platform.openai.com/docs/guides/function-calling
func (self *Client) AddToolResults(results []agent.ToolResult) {
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
		return agent.Reply{}, err
	}

	self.history = append(self.history, turn.items...)

	return agent.Reply{Calls: turn.calls(), Usage: turn.usage}, nil
}

func (self *Client) post(ctx context.Context, yield agent.Yield) (reply, error) {
	token, err := self.tokens.Token()
	if err != nil {
		return reply{}, err
	}

	stream, err := self.requests.Stream(ctx, self.endpoint(), self.requestBody(), self.headers(token))
	if err != nil {
		return reply{}, err
	}
	defer func() { _ = stream.Close() }()

	return readReply(stream, yield)
}

func (self *Client) endpoint() string {
	if self.URL != "" {
		return self.URL
	}

	return Endpoint
}

func (self *Client) model() string {
	if self.Model != "" {
		return self.Model
	}

	return Model
}

func (self *Client) effort() string {
	if self.Effort != "" {
		return self.Effort
	}

	return Effort
}

func (self *Client) requestBody() request {
	return request{
		Model:             self.model(),
		Store:             false,
		Stream:            true,
		Input:             self.history,
		Reasoning:         reasoning{Effort: self.effort(), Summary: Summary},
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

// https://platform.openai.com/docs/api-reference/responses/create
type request struct {
	Model             string         `json:"model"`                  // the model to ask
	Store             bool           `json:"store"`                  // whether the endpoint stores the response
	Tools             []functionTool `json:"tools"`                  // what the model may call
	Instructions      string         `json:"instructions,omitempty"` // what the model is told
	ParallelToolCalls bool           `json:"parallel_tool_calls"`    // whether calls may be made together

	// https://platform.openai.com/docs/api-reference/responses-streaming
	Stream bool `json:"stream"` // whether events are streamed

	Input []json.RawMessage `json:"input"` // the conversation so far

	// https://platform.openai.com/docs/guides/conversation-state
	Include []string `json:"include"` // which extra fields are returned

	// https://platform.openai.com/docs/guides/prompt-caching
	PromptCacheKey string `json:"prompt_cache_key"` // the prompt cache identity

	// https://platform.openai.com/docs/guides/function-calling
	ToolChoice string `json:"tool_choice"` // how the model chooses tools

	// https://platform.openai.com/docs/guides/reasoning
	Reasoning reasoning `json:"reasoning"` // the requested reasoning settings
}

// reasoning is how hard the model is asked to think, and how much of that thinking it summarises
// back. Without a summary asked for, none is sent.
//
// https://platform.openai.com/docs/guides/reasoning
type reasoning struct {
	Effort  string `json:"effort"`  // how hard the model should reason
	Summary string `json:"summary"` // how reasoning is reported
}

type userMessage struct {
	Role    string `json:"role"`    // who sent the message
	Content string `json:"content"` // what they said
}

type toolOutput struct {
	Type   string `json:"type"`    // the kind of item
	CallID string `json:"call_id"` // which call this answers
	Output any    `json:"output"`  // what the tool returned
}

type inputContent struct {
	Type     string `json:"type"`                // the kind of content
	Text     string `json:"text,omitempty"`      // textual content
	ImageURL string `json:"image_url,omitempty"` // an inline or remote image
	Detail   string `json:"detail,omitempty"`    // how closely the model should inspect it
}

// imageDetail is what the endpoint is asked to look at an image with. Codex names one on every
// image it returns from a tool, and this is the default it names, so it is what is asked for here.
const imageDetail = "high"

func encodeToolOutput(result agent.ToolResult) any {
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
