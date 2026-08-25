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

var Efforts = []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}

const turnTimeout = 60 * time.Minute

type Client struct {
	URL             string
	Token           string
	Model           string
	Effort          string
	MaxOutputTokens int

	instructions string
	tools        []functionTool
	history      []json.RawMessage
	requests     *req.Client
	observer     req.Observer
}

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

func (self *Client) Configure(instructions string, tools []tool.Definition) {
	self.instructions = instructions
	self.tools = describe(tools)
}

func (self *Client) ObserveHTTP(observer req.Observer) {
	self.observer = observer
	self.requests.Observe(observer)
}

func (self *Client) AddUserMessage(text string) {
	self.history = append(self.history, encode(message{Role: "user", Content: text}))
}

func (self *Client) AddToolResults(results []agent.ToolCallResult) {
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

func (self *Client) Dump() []json.RawMessage {
	return slices.Clone(self.history)
}

func (self *Client) Load(messages []json.RawMessage) {
	self.history = slices.Clone(messages)
}

func (self *Client) Send(ctx context.Context, yield agent.Yield) (agent.Reply, error) {
	stream, _, err := self.requests.Stream(ctx, self.URL, self.requestBody(), self.headers())
	if err != nil {
		return agent.Reply{}, err
	}
	defer func() { _ = stream.Close() }()

	reply, err := readReply(stream, yield)
	if err != nil {
		if reply.hasSpoken() {
			self.history = append(self.history, encode(reply.prose()))
		}

		return agent.Reply{}, err
	}

	if !reply.isEmpty() {
		self.history = append(self.history, encode(reply.message()))
	}

	return agent.Reply{Calls: reply.calls(), Usage: reply.usage}, nil
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
		StreamOptions:   streamOptions{IncludeUsage: true},
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
	encoded, _ := json.Marshal(value) //nolint:errchkjson // the wire values are plain structs
	return encoded
}

type request struct {
	Model             string            `json:"model"`
	Messages          []json.RawMessage `json:"messages"`
	Stream            bool              `json:"stream"`
	StreamOptions     streamOptions     `json:"stream_options"`
	Tools             []functionTool    `json:"tools,omitempty"`
	ToolChoice        string            `json:"tool_choice,omitempty"`
	ParallelToolCalls *bool             `json:"parallel_tool_calls,omitempty"`
	ReasoningEffort   string            `json:"reasoning_effort,omitempty"`

	MaxOutputTokens int `json:"max_completion_tokens"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
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
	for i, definition := range tools {
		described[i] = functionTool{
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

func ToolsSize(tools []tool.Tool) int {
	definitions := make([]tool.Definition, len(tools))
	for i, offeredTool := range tools {
		definitions[i] = tool.Describe(offeredTool)
	}

	encodedTools, _ := json.Marshal(describe(definitions)) //nolint:errchkjson // all fields have safe encoders
	return len(encodedTools)
}

const attachmentNotice = "Attached image(s) from tool result:"

const emptyOutputNotice = "(no tool output)"

func toolResultText(result agent.ToolCallResult) string {
	switch {
	case result.Output != "":
		return result.Output
	case result.Image.MediaType != "" && len(result.Image.Data) > 0:
		return "(see attached image)"
	default:
		return emptyOutputNotice
	}
}

func imagePart(result agent.ToolCallResult) (contentPart, bool) {
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
