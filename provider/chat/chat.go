// Package chat is a provider speaking the OpenAI Chat Completions API.
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

const turnTimeout = 60 * time.Minute

// Client speaks the Chat Completions API: one request per turn, answered as a stream of chunks.
type Client struct {
	URL    string // the endpoint address
	Model  string // the model to ask
	Effort string // how hard the model should reason
	Token  string // the bearer token

	instructions string
	tools        []functionTool
	history      []json.RawMessage
	requests     *req.Client
}

// New builds a client for an OpenAI-compatible Chat Completions endpoint.
func New(url string, model string, effort string, token string) *Client {
	return &Client{
		URL:      url,
		Model:    model,
		Effort:   effort,
		Token:    token,
		requests: req.New(turnTimeout),
	}
}

// Configure takes what every request in the session carries.
func (self *Client) Configure(instructions string, tools []tool.Definition) {
	self.instructions = instructions
	self.tools = describe(tools)
}

// ObserveHTTP attaches an observer to session requests.
func (self *Client) ObserveHTTP(observer req.Observer) {
	self.requests.Observe(observer)
}

// AddUserMessage appends a prompt to the conversation.
func (self *Client) AddUserMessage(prompt string) {
	self.history = append(self.history, encode(message{Role: "user", Content: prompt}))
}

// AddToolResults appends this turn's tool call results to the conversation.
func (self *Client) AddToolResults(results []agent.ToolResult) {
	for _, result := range results {
		self.history = append(self.history, encode(message{
			Role:       "tool",
			ToolCallID: result.ID,
			Content:    encodeToolResult(result),
		}))
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

// Send posts the conversation so far and reads the response.
func (self *Client) Send(ctx context.Context, yield agent.Yield) (agent.Reply, error) {
	stream, err := self.requests.Stream(ctx, self.URL, self.requestBody(), self.headers())
	if err != nil {
		return agent.Reply{}, err
	}
	defer func() { _ = stream.Close() }()

	answer, err := readReply(stream, yield)
	if err != nil {
		return agent.Reply{}, err
	}

	self.history = append(self.history, encode(answer.message()))
	return agent.Reply{Calls: answer.calls()}, nil
}

func (self *Client) requestBody() request {
	messages := make([]json.RawMessage, 0, len(self.history)+1)
	if self.instructions != "" {
		messages = append(messages, encode(message{Role: "system", Content: self.instructions}))
	}
	messages = append(messages, self.history...)

	return request{
		Model:             self.Model,
		Messages:          messages,
		Stream:            true,
		Tools:             self.tools,
		ToolChoice:        "auto",
		ParallelToolCalls: true,
		ReasoningEffort:   self.Effort,
	}
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
	ToolChoice        string            `json:"tool_choice,omitempty"`
	ParallelToolCalls bool              `json:"parallel_tool_calls,omitempty"`
	ReasoningEffort   string            `json:"reasoning_effort,omitempty"`
}

type message struct {
	Role             string     `json:"role"`
	Content          string     `json:"content"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
	ToolCalls        []toolCall `json:"tool_calls,omitempty"`
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

func encodeToolResult(result agent.ToolResult) string {
	if result.Image.MediaType == "" || len(result.Image.Data) == 0 {
		return result.Output
	}

	image := fmt.Sprintf(
		"data:%s;base64,%s",
		result.Image.MediaType,
		base64.StdEncoding.EncodeToString(result.Image.Data),
	)
	if result.Output == "" {
		return image
	}
	return result.Output + "\n\n" + image
}
