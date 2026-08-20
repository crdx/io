// Package chat is a provider speaking the OpenAI Chat Completions API, against any endpoint that
// claims to serve it.
package chat

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/internal/req"
	"crdx.org/io/tool"
)

// Efforts are how hard the model may be asked to work, least to most. An OpenAI-compatible endpoint
// takes these as reasoning_effort, and what any one model behind it honours is its own business.
//
// reference/chat-completions.md
var Efforts = []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}

const turnTimeout = 60 * time.Minute

// Client speaks the Chat Completions API: one request per turn, answered as a stream of chunks.
type Client struct {
	URL             string
	Token           string
	Model           string
	Effort          string
	MaxOutputTokens int // the ceiling on reasoning and answer together

	instructions string
	tools        []functionTool
	history      []json.RawMessage
	requests     *req.Client
	observer     req.Observer
}

// New builds a client for an OpenAI-compatible Chat Completions endpoint, asking the given model at
// the given effort. Nothing here has a default: which model to ask, how hard, and how much it may
// write are the caller's to decide and this package's to carry out, so a client is refused rather
// than built where any of them is missing.
func New(
	url string,
	token string,
	model string,
	effort string,
	maxOutputTokens int,
) (*Client, error) {
	client := &Client{
		URL:             url,
		Token:           token,
		Model:           model,
		Effort:          effort,
		MaxOutputTokens: maxOutputTokens,
		requests:        req.New(turnTimeout),
	}

	if err := client.settled(); err != nil {
		return nil, err
	}

	return client, nil
}

// Configure takes what every request in the session carries.
func (self *Client) Configure(instructions string, tools []tool.Definition) {
	self.instructions = instructions
	self.tools = describe(tools)
}

// ObserveHTTP attaches an observer to session requests.
func (self *Client) ObserveHTTP(observer req.Observer) {
	self.observer = observer
	self.requests.Observe(observer)
}

// AddUserMessage appends a prompt to the conversation.
func (self *Client) AddUserMessage(prompt string) {
	self.history = append(self.history, encode(message{Role: "user", Content: prompt}))
}

// AddToolResults appends this turn's tool call results to the conversation. A tool message takes
// only a string, so any images a round returned follow it in a user message of their own, which is
// the only place this API accepts one.
func (self *Client) AddToolResults(results []agent.ToolResult) {
	var images []contentPart

	for _, result := range results {
		self.history = append(self.history, encode(message{
			Role:       "tool",
			ToolCallID: result.ID,
			Content:    toolResultText(result),
		}))

		if part, carried := imagePart(result); carried {
			images = append(images, part)
		}
	}

	if len(images) > 0 {
		parts := append([]contentPart{{Type: "text", Text: attachmentNotice}}, images...)
		self.history = append(self.history, encode(partedMessage{Role: "user", Content: parts}))
	}
}

// Dump hands over the conversation so far, one message per entry.
func (self *Client) Dump() []json.RawMessage {
	return slices.Clone(self.history)
}

// Load takes a conversation back, replacing whatever this client held.
func (self *Client) Load(messages []json.RawMessage) {
	self.history = slices.Clone(messages)
}

// Send posts the conversation so far and reads the response. A turn that fails part-way keeps what
// the model said and thought before it failed, because that much was already reported to whoever
// was watching, and a conversation missing it disagrees with what they saw.
func (self *Client) Send(ctx context.Context, yield agent.Yield) (agent.Reply, error) {
	stream, err := self.requests.Stream(ctx, self.URL, self.requestBody(), self.headers())
	if err != nil {
		return agent.Reply{}, err
	}
	defer func() { _ = stream.Close() }()

	answer, err := readReply(stream, yield)
	if err != nil {
		if answer.hasSpoken() {
			self.history = append(self.history, encode(answer.prose()))
		}

		return agent.Reply{}, err
	}

	if !answer.isEmpty() {
		self.history = append(self.history, encode(answer.message()))
	}

	return agent.Reply{Calls: answer.calls()}, nil
}

func (self *Client) settled() error {
	for _, setting := range []struct {
		name  string
		value string
	}{
		{"URL", self.URL},
		{"Model", self.Model},
		{"Token", self.Token},
	} {
		if setting.value == "" {
			return fmt.Errorf("chat: %s is empty", setting.name)
		}
	}

	if self.MaxOutputTokens <= 0 {
		return fmt.Errorf("chat: MaxOutputTokens is %d, and must be above zero", self.MaxOutputTokens)
	}

	return nil
}

func (self *Client) requestBody() request {
	messages := make([]json.RawMessage, 0, len(self.history)+1)
	if self.instructions != "" {
		messages = append(messages, encode(message{Role: "system", Content: self.instructions}))
	}
	messages = append(messages, self.history...)

	body := request{
		Model:           self.Model,
		Messages:        messages,
		Stream:          true,
		Tools:           self.tools,
		ReasoningEffort: self.Effort,
		MaxOutputTokens: self.MaxOutputTokens,
	}

	if len(self.tools) > 0 {
		parallel := true
		body.ToolChoice = "auto"
		body.ParallelToolCalls = &parallel
	}

	return body
}

func (self *Client) headers() http.Header {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+self.Token)
	header.Set("Accept", "text/event-stream")
	return header
}

func encode(value any) json.RawMessage {
	encoded, _ := json.Marshal(value) //nolint:errchkjson // wire structs have safe encoders
	return encoded
}

type request struct {
	Model             string            `json:"model"`
	Messages          []json.RawMessage `json:"messages"`
	Stream            bool              `json:"stream"`
	Tools             []functionTool    `json:"tools,omitempty"`
	ToolChoice        string            `json:"tool_choice,omitempty"`         // omitempty: "" is not a choice the endpoint takes
	ParallelToolCalls *bool             `json:"parallel_tool_calls,omitempty"` // a pointer, because false asks for something and the endpoint defaults to true
	ReasoningEffort   string            `json:"reasoning_effort,omitempty"`

	MaxOutputTokens int `json:"max_completion_tokens"`
}

type message struct {
	Role             string     `json:"role"`
	Content          string     `json:"content"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
	ToolCalls        []toolCall `json:"tool_calls,omitempty"`
}

type partedMessage struct {
	Role    string        `json:"role"`
	Content []contentPart `json:"content"`
}

type contentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *imageURL `json:"image_url,omitempty"`
}

type imageURL struct {
	URL string `json:"url"`
}

type functionTool struct {
	Type     string       `json:"type"`
	Function functionSpec `json:"function"`
}

type functionSpec struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  tool.Schema `json:"parameters"`
}

func describe(tools []tool.Definition) []functionTool {
	described := make([]functionTool, len(tools))
	for index, definition := range tools {
		described[index] = functionTool{
			Type: "function",
			Function: functionSpec{
				Name:        definition.Name,
				Description: definition.Description,
				Parameters:  definition.Schema,
			},
		}
	}
	return described
}

// ToolsSize is the number of bytes the tools occupy in their provider wire representation.
func ToolsSize(tools []tool.Tool) int {
	definitions := make([]tool.Definition, len(tools))
	for index, offeredTool := range tools {
		definitions[index] = tool.Describe(offeredTool)
	}

	encodedTools, _ := json.Marshal(describe(definitions)) //nolint:errchkjson // all fields have safe encoders
	return len(encodedTools)
}

const attachmentNotice = "Attached image(s) from tool result:"

const emptyOutputNotice = "(no tool output)"

func toolResultText(result agent.ToolResult) string {
	switch {
	case result.Output != "":
		return result.Output
	case result.Image.MediaType != "" && len(result.Image.Data) > 0:
		return "(see attached image)"
	default:
		return emptyOutputNotice
	}
}

func imagePart(result agent.ToolResult) (contentPart, bool) {
	if result.Image.MediaType == "" || len(result.Image.Data) == 0 {
		return contentPart{}, false
	}

	return contentPart{
		Type: "image_url",
		ImageURL: &imageURL{
			URL: fmt.Sprintf(
				"data:%s;base64,%s",
				result.Image.MediaType,
				base64.StdEncoding.EncodeToString(result.Image.Data),
			),
		},
	}, true
}
