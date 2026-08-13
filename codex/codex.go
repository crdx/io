package codex

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/internal/req"
	"crdx.org/io/tool"
)

// —————————————————————————————————————————————————————————————————————————————————————————————————
// mega:allow-file comment-lines
// —————————————————————————————————————————————————————————————————————————————————————————————————

// Where the conversation is held, and as what.
//
// https://platform.openai.com/docs/api-reference/responses/create
const (
	Endpoint   = "https://chatgpt.com/backend-api/codex/responses"
	Model      = "gpt-5.6-sol"
	Originator = "io"
)

const turnTimeout = 30 * time.Minute

// Client speaks the Responses API: one request per turn, answered as a stream of events.
//
// https://platform.openai.com/docs/api-reference/responses/create
// https://platform.openai.com/docs/api-reference/responses-streaming
// https://platform.openai.com/docs/guides/conversation-state
type Client struct {
	URL string

	tokens       TokenSource
	instructions string
	tools        []tool.Definition
	session      string
	history      []json.RawMessage
	requests     *req.Client
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
	return New(Stored())
}

// Configure takes what every request in the session carries.
//
// https://platform.openai.com/docs/guides/function-calling
func (self *Client) Configure(instructions string, tools []tool.Definition) {
	self.instructions = instructions
	self.tools = tools
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
			Output: result.Output,
		}))
	}
}

func encodeItem(item any) json.RawMessage {
	encoded, _ := json.Marshal(item) //nolint:errchkjson // a struct of strings cannot fail

	return encoded
}

// Send posts the conversation so far and reads the response.
//
// https://platform.openai.com/docs/api-reference/responses-streaming
func (self *Client) Send() (agent.Reply, error) {
	answered, err := self.post()
	if err != nil {
		return agent.Reply{Answer: answered.answer}, err
	}

	self.history = append(self.history, answered.items...)

	return agent.Reply{Answer: answered.answer, Calls: answered.calls()}, nil
}

func (self *Client) post() (reply, error) {
	token, err := self.tokens.Token()
	if err != nil {
		return reply{}, err
	}

	stream, err := self.requests.Stream(self.endpoint(), self.requestBody(), self.headers(token))
	if err != nil {
		return reply{}, err
	}
	defer func() { _ = stream.Close() }()

	return consume(stream)
}

func (self *Client) endpoint() string {
	if self.URL != "" {
		return self.URL
	}

	return Endpoint
}

func (self *Client) requestBody() request {
	return request{
		Model:             Model,
		Store:             false,
		Stream:            true,
		Input:             self.history,
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
	Model             string            `json:"model"`
	Store             bool              `json:"store"`
	Tools             []tool.Definition `json:"tools"`
	Instructions      string            `json:"instructions,omitempty"`
	ParallelToolCalls bool              `json:"parallel_tool_calls"`

	// https://platform.openai.com/docs/api-reference/responses-streaming
	Stream bool `json:"stream"`

	Input []json.RawMessage `json:"input"`

	// https://platform.openai.com/docs/guides/conversation-state
	Include []string `json:"include"`

	// https://platform.openai.com/docs/guides/prompt-caching
	PromptCacheKey string `json:"prompt_cache_key"`

	// https://platform.openai.com/docs/guides/function-calling
	ToolChoice string `json:"tool_choice"`
}

type userMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type toolOutput struct {
	Type   string `json:"type"`
	CallID string `json:"call_id"`
	Output string `json:"output"`
}

func newToken() string {
	buffer := make([]byte, 32)
	_, _ = rand.Read(buffer)

	return hex.EncodeToString(buffer)
}
