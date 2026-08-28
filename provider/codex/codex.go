package codex

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"slices"
	"sync"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/internal/req"
	"crdx.org/io/tool"
)

const (
	Endpoint = "https://chatgpt.com/backend-api/codex/responses"
	Summary  = "auto"

	Originator = "io"
)

var Efforts = []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}

const (
	turnTimeout  = 60 * time.Minute
	asideTimeout = 30 * time.Second
)

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
	observer     req.Observer

	usageMutex   sync.Mutex
	usageWindows []agent.UsageWindow
}

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

// UseSession names the conversation this client is holding, so that the key its requests are cached
// under is the same one every time the conversation is resumed.
func (self *Client) UseSession(name string) {
	self.session = sessionToken(name)
}

func Auth(model string, effort string) (*Client, error) {
	return New(StoredCredentials(), model, effort)
}

func (self *Client) Configure(instructions string, tools []tool.Definition) {
	self.instructions = instructions
	self.tools = describe(tools)
}

func (self *Client) ObserveHTTP(observer req.Observer) {
	self.observer = observer
	self.requests.Observe(observer)
	if source, ok := self.tokens.(observedTokenSource); ok {
		source.observeHTTP(observer)
	}
}

func (self *Client) AddUserMessage(text string) {
	self.history = append(self.history, encodeItem(userMessage{Role: "user", Content: text}))
}

func (self *Client) AddToolResults(results []agent.ToolCallResult) {
	for _, result := range results {
		self.history = append(self.history, encodeItem(toolOutput{
			Type:   "function_call_output",
			CallID: result.ID,
			Output: encodeToolOutput(result),
		}))
	}
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
		self.history = append(self.history, reply.prose(isFinalFailure(err))...)

		return agent.Reply{}, err
	}

	self.history = append(self.history, reply.items...)

	return agent.Reply{Calls: reply.calls(), Usage: reply.usage}, nil
}

func (self *Client) observedRequests() *req.Client {
	client := req.New(asideTimeout)
	if self.observer != nil {
		client.Observe(self.observer)
	}

	return client
}

func (self *Client) post(ctx context.Context, yield agent.Yield) (reply, error) {
	token, err := self.tokens.Token()
	if err != nil {
		return reply{}, err
	}

	stream, responseHeader, err := self.requests.Stream(ctx, self.URL, self.requestBody(), self.headers(token))
	if err != nil {
		return reply{}, err
	}
	defer func() { _ = stream.Close() }()

	self.recordUsageWindows(responseHeader, time.Now())

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
	return requestHeaders(token, self.session)
}

func userAgent() string {
	return fmt.Sprintf("io (%s; %s)", runtime.GOOS, runtime.GOARCH)
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

func sessionToken(name string) string {
	sum := sha256.Sum256([]byte(name))

	return hex.EncodeToString(sum[:])
}
