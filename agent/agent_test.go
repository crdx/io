package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/tool"
)

type callProvider struct {
	sent    int                // how many turns were sent
	results []agent.ToolResult // the results received
}

func (self *callProvider) Configure(string, []tool.Definition) {}
func (self *callProvider) AddUserMessage(string)               {}

func (self *callProvider) AddToolResults(results []agent.ToolResult) {
	self.results = append(self.results, results...)
}

func (self *callProvider) Send(_ context.Context, yield agent.Yield) (agent.Reply, error) {
	self.sent++

	if self.sent > 1 {
		return agent.Reply{}, nil
	}

	yield(agent.Event{Kind: agent.Text, Text: "thinking out loud"})

	return agent.Reply{Calls: []agent.ToolCall{
		{ID: "a", Name: "noop", Arguments: `{}`},
		{ID: "b", Name: "noop", Arguments: `{}`},
	}}, nil
}

func noop() tool.Tool {
	return tool.Concurrent(tool.Define("noop", "", tool.Schema{},
		func(struct{}) (string, string) { return "", "" },
		func(context.Context, struct{}) (string, error) { return "done", nil },
	))
}

func resultOutputs(provider *callProvider) []string {
	var outputs []string

	for _, result := range provider.results {
		outputs = append(outputs, result.ID+":"+result.Output)
	}

	return outputs
}

// A turn dropped partway still answers every call the model made, because the next request carries
// the whole conversation and the endpoint rejects a call with no result.
func TestStreamAnswersEveryCallOfAnAbandonedTurn(t *testing.T) {
	tests := map[string]agent.Kind{
		"dropped while streaming text": agent.Text,
		"dropped on the first call":    agent.Call,
	}

	for name, stopOn := range tests {
		t.Run(name, func(t *testing.T) {
			provider := &callProvider{}
			assistant := agent.New("", provider, []tool.Tool{noop()})

			for event, err := range assistant.Stream(t.Context(), "go") {
				if err != nil {
					t.Fatal(err)
				}

				if event.Kind == stopOn {
					break
				}
			}

			want := []string{"a:" + agent.CancelledOutput, "b:" + agent.CancelledOutput}

			if got := resultOutputs(provider); !slices.Equal(got, want) {
				t.Errorf("got %v, want %v", got, want)
			}
		})
	}
}

// Every call is away before the first of them reports, so a turn dropped once they are running is
// answered with what they came back with. Saying they were cancelled would be telling the model
// something untrue about work it is about to be shown.
func TestStreamAnswersWithWhateverRanBeforeTheTurnWasDropped(t *testing.T) {
	provider := &callProvider{}
	assistant := agent.New("", provider, []tool.Tool{noop()})

	for event, err := range assistant.Stream(t.Context(), "go") {
		if err != nil {
			t.Fatal(err)
		}

		if event.Kind == agent.Result {
			break
		}
	}

	want := []string{"a:done", "b:done"}
	if got := resultOutputs(provider); !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// Both calls are in flight at once, or neither gets past the barrier. Run one after the other, the
// first would wait on a release that only the second can bring about, and every call times out.
func TestStreamRunsEveryCallOfAReplyAtOnce(t *testing.T) {
	var arrivalBarrier sync.WaitGroup

	arrivalBarrier.Add(2)

	release := make(chan struct{})

	go func() {
		arrivalBarrier.Wait()
		close(release)
	}()

	var concurrentCalls atomic.Int32

	barrierTool := tool.Concurrent(tool.Define("noop", "", tool.Schema{},
		func(struct{}) (string, string) { return "", "" },
		func(context.Context, struct{}) (string, error) {
			arrivalBarrier.Done()

			select {
			case <-release:
				concurrentCalls.Add(1)
			case <-time.After(time.Second):
			}

			return "done", nil
		},
	))

	provider := &callProvider{}

	if _, err := agent.New("", provider, []tool.Tool{barrierTool}).Send(t.Context(), "go"); err != nil {
		t.Fatal(err)
	}

	if got := concurrentCalls.Load(); got != 2 {
		t.Errorf("expected both calls to be in flight at once, got %d", got)
	}
}

// A tool that says nothing about running alongside others is left on its own, so a call that writes
// can never be in flight beside one that reads the same thing.
func TestStreamLeavesACallThatIsNotConcurrentOnItsOwn(t *testing.T) {
	var mutex sync.Mutex

	runningCalls, maxRunningCalls := 0, 0

	serialTool := tool.Define("noop", "", tool.Schema{},
		func(struct{}) (string, string) { return "", "" },
		func(context.Context, struct{}) (string, error) {
			mutex.Lock()
			runningCalls++
			maxRunningCalls = max(maxRunningCalls, runningCalls)
			mutex.Unlock()

			time.Sleep(20 * time.Millisecond)

			mutex.Lock()
			runningCalls--
			mutex.Unlock()

			return "done", nil
		},
	)

	provider := &callProvider{}

	if _, err := agent.New("", provider, []tool.Tool{serialTool}).Send(t.Context(), "go"); err != nil {
		t.Fatal(err)
	}

	if maxRunningCalls != 1 {
		t.Errorf("expected the calls to be run one at a time, got %d at once", maxRunningCalls)
	}

	if got := resultOutputs(provider); !slices.Equal(got, []string{"a:done", "b:done"}) {
		t.Errorf("expected both to be answered, got %v", got)
	}
}

// What a call took is measured where it is run, so it is the tool's own time rather than however
// long a display took to notice, and it is written down with the result it belongs to.
func TestAResultSaysHowLongItsCallTook(t *testing.T) {
	const slept = 50 * time.Millisecond

	slow := tool.Concurrent(tool.Define("noop", "", tool.Schema{},
		func(struct{}) (string, string) { return "", "" },
		func(context.Context, struct{}) (string, error) {
			time.Sleep(slept)
			return "done", nil
		},
	))

	assistant := agent.New("", &callProvider{}, []tool.Tool{slow})

	timedResults := 0

	for event, err := range assistant.Stream(t.Context(), "go") {
		if err != nil {
			t.Fatal(err)
		}

		if event.Kind == agent.Result {
			timedResults++

			if event.Took < slept {
				t.Errorf("expected the call to have taken at least %s, got %s", slept, event.Took)
			}
		}
	}

	if timedResults != 2 {
		t.Errorf("expected both calls to have been timed, got %d", timedResults)
	}
}

func TestStreamAnswersEveryCallOfAFinishedTurn(t *testing.T) {
	provider := &callProvider{}
	assistant := agent.New("", provider, []tool.Tool{noop()})

	for _, err := range assistant.Stream(t.Context(), "go") {
		if err != nil {
			t.Fatal(err)
		}
	}

	want := []string{"a:done", "b:done"}
	if got := resultOutputs(provider); !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

type oneCallProvider struct {
	sent    int                // how many turns were sent
	results []agent.ToolResult // the results received
}

func (self *oneCallProvider) Configure(string, []tool.Definition) {}
func (self *oneCallProvider) AddUserMessage(string)               {}
func (self *oneCallProvider) Dump() []json.RawMessage             { return nil }
func (self *oneCallProvider) Load([]json.RawMessage)              {}

func (self *oneCallProvider) AddToolResults(results []agent.ToolResult) {
	self.results = append(self.results, results...)
}

func (self *oneCallProvider) Send(_ context.Context, _ agent.Yield) (agent.Reply, error) {
	if self.sent++; self.sent > 1 {
		return agent.Reply{}, nil
	}

	return agent.Reply{Calls: []agent.ToolCall{{ID: "a", Name: "failing", Arguments: `{}`}}}, nil
}

func failingTool() tool.Tool {
	return tool.Define("failing", "", tool.Schema{},
		func(struct{}) (string, string) { return "", "" },
		func(context.Context, struct{}) (string, error) {
			return "permission denied\nexit status 1", errors.New("the command failed")
		},
	)
}

// A command that came back with something other than nought is marked as having failed, or the tick
// against it says a thing that did not happen. What it printed is the result all the same, so the
// model is handed that rather than only the reason.
func TestACallThatFailedWithSomethingToSaySaysItAndIsMarkedFailed(t *testing.T) {
	provider := &oneCallProvider{}
	assistant := agent.New("", provider, []tool.Tool{failingTool()})

	var results []agent.Event

	for event, err := range assistant.Stream(t.Context(), "go") {
		if err != nil {
			t.Fatal(err)
		}

		if event.Kind == agent.Result {
			results = append(results, event)
		}
	}

	if len(results) != 1 {
		t.Fatalf("expected one result, got %d", len(results))
	}

	if !results[0].Failed {
		t.Error("expected the call to be marked as having failed")
	}

	if !strings.Contains(results[0].Text, "permission denied") {
		t.Errorf("expected what the command printed, got %q", results[0].Text)
	}
}

type notingProvider struct {
	messages []string // what was added, in the order it was added
}

func (self *notingProvider) Configure(string, []tool.Definition) {}
func (self *notingProvider) AddUserMessage(text string)          { self.messages = append(self.messages, text) }
func (self *notingProvider) AddToolResults([]agent.ToolResult)   {}

func (self *notingProvider) Send(context.Context, agent.Yield) (agent.Reply, error) {
	return agent.Reply{}, nil
}

// A note is read before whatever is asked next, and is not a turn of its own.
func TestANoteGoesAheadOfTheNextPrompt(t *testing.T) {
	provider := &notingProvider{}
	self := agent.New("", provider, nil)

	self.Note("something changed")

	if len(provider.messages) != 1 {
		t.Fatalf("expected the note to have been added, got %v", provider.messages)
	}

	if _, err := self.Send(t.Context(), "do the thing"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{"something changed", "do the thing"}

	if !slices.Equal(provider.messages, want) {
		t.Errorf("expected %v, got %v", want, provider.messages)
	}
}
