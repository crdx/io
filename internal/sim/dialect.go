package sim

import (
	"net/http"
	"strings"
	"time"
)

// The wire formats the endpoint answers in. These name formats rather than providers, because the
// two are different things: several providers can speak one format, and which of them speak which
// is no business of something standing in for an endpoint.
const (
	Responses   = "responses"   // the OpenAI Responses API
	Completions = "completions" // the OpenAI Chat Completions API
	Messages    = "messages"    // the Anthropic Messages API
)

// Dialect is one wire format the endpoint answers in. A scenario says what the model does; a
// dialect says how that reaches the client, which is the only part that differs between the three.
//
// A provider speaking an awkward variation of a format gets a dialect of its own, forked from the
// one it varies from, rather than the shared one growing a flag for it. Two formats that are
// nearly the same are still two formats, and reading either should not mean reading around the
// other.
type Dialect interface {
	Name() string

	Path() string

	Read(request *http.Request, raw []byte) (Request, bool)

	Check(scenario *Scenario, asked Request) string

	Play(stream *Stream, scenario *Scenario, turn Turn)

	Exhausted(stream *Stream, message string)
}

// Dialects are the wire formats the endpoint answers in, all of them at once: which one a request
// gets is decided by where it was posted, so one running endpoint stands in for every provider.
func Dialects() []Dialect {
	return []Dialect{newResponsesDialect(), completionsDialect{}, messagesDialect{}}
}

// Stream is the wire one turn is played onto. It paces itself the way the scenario asked and hands
// out the identifiers a format needs to attach to things. What it knows about a format ends there:
// what to write, and when a response is over, is the dialect's to say.
type Stream struct {
	send   func(string) // writes one event
	pace   time.Duration
	nextID func(prefix string) string
}

// Send writes one event.
func (self *Stream) Send(event string) {
	self.send(event)
}

// Type writes text a word at a time, waiting between words, which is how an answer arrives from a
// real endpoint and is what a display drawing one has to cope with.
func (self *Stream) Type(as func(string) string, text string) {
	for index, word := range strings.SplitAfter(text, " ") {
		if word == "" {
			continue
		}

		if index > 0 {
			time.Sleep(self.pace)
		}

		self.Send(as(word))
	}
}

// ID hands out an identifier under the given prefix, unique for the life of the endpoint.
func (self *Stream) ID(prefix string) string {
	return self.nextID(prefix)
}

func unansweredCall(input []Entry) string {
	answered := map[string]bool{}

	for _, entry := range input {
		if entry.Type == CallOutput {
			answered[entry.CallID] = true
		}
	}

	for _, entry := range input {
		if entry.Type == CallMade && !answered[entry.CallID] {
			return entry.CallID
		}
	}

	return ""
}

func assistantTurns(roles []string) int {
	var count int

	for _, role := range roles {
		if role == "assistant" {
			count++
		}
	}

	return count
}
