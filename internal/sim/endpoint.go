package sim

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Endpoint answers requests as the scenario demands, in whichever provider API they arrive in.
type Endpoint struct {
	scenario *Scenario // what the endpoint acts out
	dialects []Dialect // the APIs it answers in

	mutex    sync.Mutex     // guards endpoint state
	requests []Request      // the requests received
	sessions map[string]int // how many turns each conversation has taken
	nextID   int            // the next identifier
}

const exhausted = "The scenario has nothing more to say."

// New builds an endpoint over a scenario, answering in every API a provider here speaks.
func New(scenario *Scenario) *Endpoint {
	return &Endpoint{scenario: scenario, dialects: Dialects(), sessions: map[string]int{}}
}

// The kinds of item a conversation holds, named the same way whichever API carried them, so that
// what a test asks about a conversation does not depend on which provider sent it.
const (
	Message    = "message"
	CallMade   = "function_call"
	CallOutput = "function_call_output"
)

// Request is one request as the endpoint understood it.
type Request struct {
	API          string // which provider API it arrived in
	Session      string
	Model        string
	Instructions string
	Streaming    bool
	IncludeUsage bool
	IsStored     bool
	Turn         int
	Input        []Entry
	Tools        []string
}

// Entry is one item of the conversation the request carried.
type Entry struct {
	Type      string
	Role      string
	Content   string
	CallID    string
	Name      string
	Arguments string
	Output    string
	Raw       json.RawMessage
}

// Requests is every request the endpoint has answered, in order.
func (self *Endpoint) Requests() []Request {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	return append([]Request(nil), self.requests...)
}

// Addresses is where each wire format this endpoint answers in is posted to, below the given base,
// keyed by format. Which provider to point at which of them is the caller's to know: a format is
// spoken by however many providers speak it, and this end of it cannot tell them apart.
func (self *Endpoint) Addresses(base string) map[string]string {
	addresses := make(map[string]string, len(self.dialects))

	for _, dialect := range self.dialects {
		addresses[dialect.Name()] = strings.TrimSuffix(base, "/") + versionPrefix + dialect.Path()
	}

	return addresses
}

const (
	versionPrefix    = "/v1"
	modelsPath       = "/v1/models"
	ollamaModelsPath = "/api/tags"
	registryPath     = "/models.dev/api.json"
)

func (self *Endpoint) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	path := request.URL.Path

	switch {
	case strings.HasSuffix(path, registryPath):
		self.serveRegistry(writer, request)

		return

	case strings.HasSuffix(path, modelsPath):
		self.serveListing(writer)

		return

	case strings.HasSuffix(path, ollamaModelsPath):
		self.serveOllamaListing(writer)

		return
	}

	dialect, isSpoken := self.dialectFor(path)
	if !isSpoken {
		refuse(writer, http.StatusNotFound, "nothing here speaks for "+path)

		return
	}

	self.answer(writer, request, dialect)
}

func (self *Endpoint) dialectFor(path string) (Dialect, bool) {
	for _, dialect := range self.dialects {
		if strings.HasSuffix(path, dialect.Path()) {
			return dialect, true
		}
	}

	return nil, false
}

func (self *Endpoint) answer(writer http.ResponseWriter, request *http.Request, dialect Dialect) {
	raw, err := io.ReadAll(request.Body)
	if err != nil {
		refuse(writer, http.StatusBadRequest, "the request could not be read")

		return
	}

	askedRequest, readable := dialect.Read(request, raw)
	if !readable {
		refuse(writer, http.StatusBadRequest, "the request was not json")

		return
	}

	askedRequest.Turn = self.take(askedRequest.Session)

	self.record(askedRequest)

	if self.scenario.Strict {
		if failure := dialect.Check(self.scenario, askedRequest); failure != "" {
			refuse(writer, http.StatusBadRequest, failure)

			return
		}
	}

	turn, found := self.scenario.turn(askedRequest.Turn)

	if found && turn.Status != 0 {
		if turn.RetryAfter.Duration > 0 {
			writer.Header().Set("Retry-After", strconv.Itoa(int(turn.RetryAfter.Seconds())))
		}

		refuse(writer, turn.Status, "the endpoint is having a moment")

		return
	}

	stream := self.open(writer, turn)

	if !found {
		dialect.Exhausted(stream, exhausted)

		return
	}

	time.Sleep(self.scenario.delay(turn))

	dialect.Play(stream, self.scenario, turn)
}

func (self *Endpoint) open(writer http.ResponseWriter, turn Turn) *Stream {
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.WriteHeader(http.StatusOK)

	flush := http.NewResponseController(writer)

	return &Stream{
		send: func(event string) {
			_, _ = io.WriteString(writer, "data: "+event+"\n\n")
			_ = flush.Flush()
		},
		pace:   self.scenario.pace(turn),
		nextID: self.identifier,
	}
}

func (self *Endpoint) identifier(prefix string) string {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.nextID++

	return fmt.Sprintf("%s_%d", prefix, self.nextID)
}

func (self *Endpoint) take(session string) int {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	turn := self.sessions[session]
	self.sessions[session] = turn + 1

	return turn
}

func (self *Endpoint) record(askedRequest Request) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.requests = append(self.requests, askedRequest)
}

func flatten(content any) string {
	switch typedContent := content.(type) {
	case string:
		return typedContent

	case []any:
		var sentence strings.Builder

		for _, part := range typedContent {
			if block, isObject := part.(map[string]any); isObject {
				if text, isText := block["text"].(string); isText {
					sentence.WriteString(text)
				}
			}
		}

		return sentence.String()
	}

	return ""
}

type refusal struct {
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

func refuse(writer http.ResponseWriter, status int, message string) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)

	var response refusal
	response.Error.Message = message

	_ = json.NewEncoder(writer).Encode(response) //nolint:errchkjson // a struct of strings cannot fail
}
