package codex

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"time"

	"crdx.org/io/harness"
	"crdx.org/io/tool"
)

// Where the conversation is held, and as what.
const (
	Endpoint   = "https://chatgpt.com/backend-api/codex/responses"
	Model      = "gpt-5.6-sol"
	Originator = "io"
)

// Client speaks the Responses API: one request per turn, answered as a stream of events. It owns
// the conversation, since the endpoint wants the whole of it back every time.
type Client struct {
	URL string

	tokens       TokenSource
	instructions string
	tools        []tool.Definition
	session      string
	history      []json.RawMessage
	http         *http.Client
}

// New builds a client that authorises every request with the given source.
func New(tokens TokenSource) *Client {
	return &Client{
		tokens:  tokens,
		session: newToken(),
		http:    &http.Client{Timeout: 30 * time.Minute},
	}
}

// Auth is a client on the credentials the login command stored, which is what a caller with no
// opinion about where credentials come from wants.
func Auth() *Client {
	return New(Stored())
}

// Configure takes what every request in the session carries.
func (self *Client) Configure(instructions string, tools []tool.Definition) {
	self.instructions = instructions
	self.tools = tools
}

// AddUserMessage appends a prompt to the conversation, as the endpoint expects it in the input
// list.
func (self *Client) AddUserMessage(prompt string) {
	self.history = append(self.history, encodeItem(userMessage{Role: "user", Content: prompt}))
}

// AddToolResults appends this turn's tool call results to the conversation.
func (self *Client) AddToolResults(results []harness.ToolResult) {
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
func (self *Client) Send() (harness.Reply, error) {
	answered, err := self.post()
	if err != nil {
		return harness.Reply{Answer: answered.answer}, err
	}

	self.history = append(self.history, answered.items...)

	return harness.Reply{Answer: answered.answer, Calls: answered.calls()}, nil
}

func (self *Client) post() (reply, error) {
	token, err := self.tokens.Token()
	if err != nil {
		return reply{}, err
	}

	body, err := json.Marshal(self.requestBody())
	if err != nil {
		return reply{}, fmt.Errorf("encode request: %w", err)
	}

	request, err := http.NewRequest(http.MethodPost, self.endpoint(), bytes.NewReader(body))
	if err != nil {
		return reply{}, err
	}

	self.setHeaders(request, token)

	response, err := self.http.Do(request)
	if err != nil {
		return reply{}, err
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return reply{}, responseError(response)
	}

	return consume(response.Body)
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

func (self *Client) setHeaders(request *http.Request, token Token) {
	request.Header.Set("Authorization", "Bearer "+token.Access)
	request.Header.Set("Chatgpt-Account-Id", token.AccountID)
	request.Header.Set("Originator", Originator)
	request.Header.Set("Openai-Beta", "responses=experimental")
	request.Header.Set("Accept", "text/event-stream")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Session_id", self.session)
	request.Header.Set("User-Agent", fmt.Sprintf("io (%s; %s)", runtime.GOOS, runtime.GOARCH))
}

type request struct {
	Model             string            `json:"model"`
	Store             bool              `json:"store"`
	Stream            bool              `json:"stream"`
	Input             []json.RawMessage `json:"input"`
	Include           []string          `json:"include"`
	PromptCacheKey    string            `json:"prompt_cache_key"`
	ToolChoice        string            `json:"tool_choice"`
	ParallelToolCalls bool              `json:"parallel_tool_calls"`
	Tools             []tool.Definition `json:"tools"`
	Instructions      string            `json:"instructions,omitempty"`
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

func responseError(response *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(response.Body, 64*1024))

	var payload struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if json.Unmarshal(body, &payload) != nil || payload.Error.Message == "" {
		return fmt.Errorf("request failed with status %d: %s", response.StatusCode, body)
	}

	return fmt.Errorf("%s", payload.Error.Message)
}

func newToken() string {
	buffer := make([]byte, 32)
	_, _ = rand.Read(buffer)

	return hex.EncodeToString(buffer)
}
