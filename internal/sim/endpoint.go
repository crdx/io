package sim

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"crdx.org/io/internal/sim/wire"
)

// Endpoint answers requests as the scenario demands.
type Endpoint struct {
	scenario *Scenario // what the endpoint acts out

	mutex       sync.Mutex      // guards endpoint state
	sessions    map[string]*run // the conversations in progress
	requests    []Request       // the requests received
	nextSession int             // the next session number
}

type run struct {
	turn int // the next turn
}

const exhausted = "The scenario has nothing more to say."

// New builds an endpoint over a scenario.
func New(scenario *Scenario) *Endpoint {
	return &Endpoint{scenario: scenario, sessions: map[string]*run{}}
}

// Request is one request as the endpoint understood it.
type Request struct {
	Session string   // which conversation
	Model   string   // which model was asked
	Turn    int      // which scenario turn answered
	Input   []Entry  // the conversation sent
	Tools   []string // the tools offered
}

// Entry is one item of the conversation the request carried.
type Entry struct {
	Type    string          `json:"type"`    // the kind of item
	Role    string          `json:"role"`    // who sent the message
	Content string          `json:"content"` // what was said
	CallID  string          `json:"call_id"` // which call
	Name    string          `json:"name"`    // which tool
	Output  string          `json:"output"`  // what the tool returned
	Raw     json.RawMessage `json:"-"`       // the item as received
}

type body struct {
	Model          string            `json:"model"`            // the model being asked
	Stream         bool              `json:"stream"`           // whether events are requested
	Store          bool              `json:"store"`            // whether storage is requested
	Instructions   string            `json:"instructions"`     // what the model was told
	PromptCacheKey string            `json:"prompt_cache_key"` // the conversation key
	Input          []json.RawMessage `json:"input"`            // the conversation sent
	Tools          []struct {
		Name string `json:"name"` // what the tool is called
	} `json:"tools"` // the tools offered
}

// Requests is every request the endpoint has answered, in order.
func (self *Endpoint) Requests() []Request {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	return append([]Request(nil), self.requests...)
}

func (self *Endpoint) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	raw, err := io.ReadAll(request.Body)
	if err != nil {
		refuse(writer, http.StatusBadRequest, "the request could not be read")
		return
	}

	var sent body

	if json.Unmarshal(raw, &sent) != nil {
		refuse(writer, http.StatusBadRequest, "the request was not json")
		return
	}

	askedRequest := self.record(request, sent)

	if failure := self.check(sent, askedRequest); failure != "" {
		refuse(writer, http.StatusBadRequest, failure)
		return
	}

	turn, found := self.scenario.turn(askedRequest.Turn)
	if !found {
		self.stream(writer, wire.Body(
			wire.Answer(exhausted),
			wire.Message(exhausted),
			wire.CompletedResponse,
		))

		return
	}

	if turn.Status != 0 {
		refuse(writer, turn.Status, "the endpoint is having a moment")
		return
	}

	self.play(writer, turn)
}

func (self *Endpoint) nextID(prefix string) string {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.nextSession++

	return fmt.Sprintf("%s_%d", prefix, self.nextSession)
}

func (self *Endpoint) record(request *http.Request, sent body) Request {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	key := sent.PromptCacheKey
	if key == "" {
		key = request.Header.Get("Session_id")
	}

	held, found := self.sessions[key]
	if !found {
		held = &run{}
		self.sessions[key] = held
	}

	askedRequest := Request{
		Session: key,
		Model:   sent.Model,
		Turn:    held.turn,
		Input:   entries(sent.Input),
	}

	for _, offeredTool := range sent.Tools {
		askedRequest.Tools = append(askedRequest.Tools, offeredTool.Name)
	}

	held.turn++

	self.requests = append(self.requests, askedRequest)

	return askedRequest
}

func entries(items []json.RawMessage) []Entry {
	read := make([]Entry, 0, len(items))

	for _, item := range items {
		var entry Entry

		_ = json.Unmarshal(item, &entry)
		entry.Raw = item

		read = append(read, entry)
	}

	return read
}

func (self *Endpoint) check(sent body, askedRequest Request) string {
	if !self.scenario.Strict {
		return ""
	}

	switch {
	case !sent.Stream:
		return "only streaming responses are supported"
	case sent.Store:
		return "this endpoint does not store conversations"
	case sent.Instructions == "":
		return "the request carried no instructions"
	case sent.Model != self.scenario.Model:
		return fmt.Sprintf("the model %q is not available", sent.Model)
	}

	answeredCalls := map[string]bool{}

	for _, entry := range askedRequest.Input {
		if entry.Type == "function_call_output" {
			answeredCalls[entry.CallID] = true
		}
	}

	for _, entry := range askedRequest.Input {
		if entry.Type == "function_call" && !answeredCalls[entry.CallID] {
			return "No tool output found for function call " + entry.CallID + "."
		}
	}

	return ""
}

func (self *Endpoint) play(writer http.ResponseWriter, turn Turn) {
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.WriteHeader(http.StatusOK)

	flush := http.NewResponseController(writer)

	send := func(event string) {
		_, _ = io.WriteString(writer, "data: "+event+"\n\n")
		_ = flush.Flush()
	}

	time.Sleep(self.scenario.delay(turn))

	for _, thought := range turn.Think {
		self.typeWords(send, wire.Thought, thought, self.scenario.pace(turn))

		send(wire.ThinkingPart(thought))
		send(wire.ReasoningItem(self.nextID("rs"), thought))
	}

	if turn.Truncate {
		return
	}

	if turn.Say != "" {
		self.typeWords(send, wire.Answer, turn.Say, self.scenario.pace(turn))
		send(wire.Message(turn.Say))
	}

	for _, call := range turn.Calls {
		send(wire.Call(self.nextID("call"), call.Name, call.Arguments))
	}

	switch {
	case turn.ErrorEvent != "":
		send(turn.ErrorEvent)
	case turn.Fail != "":
		send(wire.FailedResponse(turn.Fail))
	case turn.Incomplete:
		send(wire.IncompleteResponse)
	default:
		send(wire.CompletedResponse)
	}

	_, _ = io.WriteString(writer, wire.Done)
	_ = flush.Flush()
}

func (self *Endpoint) typeWords(
	send func(string),
	as func(string) string,
	text string,
	pace time.Duration,
) {
	for index, word := range strings.SplitAfter(text, " ") {
		if word == "" {
			continue
		}

		if index > 0 {
			time.Sleep(pace)
		}

		send(as(word))
	}
}

func (self *Endpoint) stream(writer http.ResponseWriter, turn string) {
	writer.Header().Set("Content-Type", "text/event-stream")
	_, _ = io.WriteString(writer, turn+wire.Done)
}

type refusal struct {
	Error struct {
		Message string `json:"message"` // what went wrong
	} `json:"error"` // the endpoint error
}

func refuse(writer http.ResponseWriter, status int, message string) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)

	var response refusal
	response.Error.Message = message

	_ = json.NewEncoder(writer).Encode(response) //nolint:errchkjson // a struct of strings cannot fail
}
