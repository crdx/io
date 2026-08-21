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
	sent    int
	results []agent.ToolResult
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
	return tool.Concurrent(tool.Implement(
		tool.Definition{
			Name:        "noop",
			Description: "",
			Schema:      tool.Schema{},
		},
		func(struct{}) (string, string) { return "", "" },
	).Plain(func(context.Context, struct{}) (string, error) { return "done", nil }))
}

func resultOutputs(provider *callProvider) []string {
	var outputs []string

	for _, result := range provider.results {
		outputs = append(outputs, result.ID+":"+result.Output)
	}

	return outputs
}

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

func TestStreamRunsEveryCallOfAReplyAtOnce(t *testing.T) {
	var arrivalBarrier sync.WaitGroup

	arrivalBarrier.Add(2)

	release := make(chan struct{})

	go func() {
		arrivalBarrier.Wait()
		close(release)
	}()

	var concurrentCalls atomic.Int32

	barrierTool := tool.Concurrent(tool.Implement(
		tool.Definition{
			Name:        "noop",
			Description: "",
			Schema:      tool.Schema{},
		},
		func(struct{}) (string, string) { return "", "" },
	).Plain(func(context.Context, struct{}) (string, error) {
		arrivalBarrier.Done()

		select {
		case <-release:
			concurrentCalls.Add(1)
		case <-time.After(time.Second):
		}

		return "done", nil
	}))

	provider := &callProvider{}

	if _, err := agent.New("", provider, []tool.Tool{barrierTool}).Send(t.Context(), "go"); err != nil {
		t.Fatal(err)
	}

	if got := concurrentCalls.Load(); got != 2 {
		t.Errorf("expected both calls to be in flight at once, got %d", got)
	}
}

func TestStreamLeavesACallThatIsNotConcurrentOnItsOwn(t *testing.T) {
	var mutex sync.Mutex

	runningCalls, maxRunningCalls := 0, 0

	serialTool := tool.Implement(
		tool.Definition{
			Name:        "noop",
			Description: "",
			Schema:      tool.Schema{},
		},
		func(struct{}) (string, string) { return "", "" },
	).Plain(func(context.Context, struct{}) (string, error) {
		mutex.Lock()
		runningCalls++
		maxRunningCalls = max(maxRunningCalls, runningCalls)
		mutex.Unlock()

		time.Sleep(20 * time.Millisecond)

		mutex.Lock()
		runningCalls--
		mutex.Unlock()

		return "done", nil
	})

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

func TestAResultSaysHowLongItsCallTook(t *testing.T) {
	const slept = 50 * time.Millisecond

	slow := tool.Concurrent(tool.Implement(
		tool.Definition{
			Name:        "noop",
			Description: "",
			Schema:      tool.Schema{},
		},
		func(struct{}) (string, string) { return "", "" },
	).Plain(func(context.Context, struct{}) (string, error) {
		time.Sleep(slept)
		return "done", nil
	}))

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
	call    agent.ToolCall
	sent    int
	results []agent.ToolResult
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

	call := self.call
	if call.Name == "" {
		call = agent.ToolCall{ID: "a", Name: "failing", Arguments: `{}`}
	}
	return agent.Reply{Calls: []agent.ToolCall{call}}, nil
}

func singleResult(t *testing.T, calledTool tool.Tool) agent.Event {
	t.Helper()

	provider := &oneCallProvider{call: agent.ToolCall{ID: "a", Name: calledTool.Name(), Arguments: `{}`}}
	assistant := agent.New("", provider, []tool.Tool{calledTool})

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
	return results[0]
}

func failingTool() tool.Tool {
	return tool.Implement(
		tool.Definition{
			Name:        "failing",
			Description: "",
			Schema:      tool.Schema{},
		},
		func(struct{}) (string, string) { return "", "" },
	).Plain(func(context.Context, struct{}) (string, error) {
		return "permission denied\nexit status 1", errors.New("the command failed")
	})
}

func TestACallThatFailedWithSomethingToSaySaysItAndIsMarkedFailed(t *testing.T) {
	result := singleResult(t, failingTool())

	if !result.Failed {
		t.Error("expected the call to be marked as having failed")
	}

	if !strings.Contains(result.Text, "permission denied") {
		t.Errorf("expected what the command printed, got %q", result.Text)
	}
}

func plainOutputTool(output string, executionError error) tool.Tool {
	return tool.Implement(
		tool.Definition{Name: "output", Description: "", Schema: tool.Schema{}},
		func(struct{}) (string, string) { return "", "" },
	).Plain(func(context.Context, struct{}) (string, error) {
		return output, executionError
	})
}

func statsOutputTool(output string, stats tool.Stats) tool.Tool {
	return tool.Implement(
		tool.Definition{Name: "output", Description: "", Schema: tool.Schema{}},
		func(struct{}) (string, string) { return "", "" },
	).Stats(func(context.Context, struct{}) (string, tool.Stats, error) {
		return output, stats, nil
	})
}

func requireStats(t *testing.T, result agent.Event, want tool.Stats) {
	t.Helper()

	if result.Stats == nil {
		t.Fatal("expected result stats")
	}
	if *result.Stats != want {
		t.Errorf("got stats %#v, want %#v", *result.Stats, want)
	}
}

func TestAPlainCallReceivesOutputStats(t *testing.T) {
	const output = "one\ntwo\n"
	result := singleResult(t, plainOutputTool(output, nil))

	requireStats(t, result, tool.OutputStats(output))
}

func TestAnEmptyPlainCallReceivesEmptyOutputStats(t *testing.T) {
	result := singleResult(t, plainOutputTool("", nil))

	requireStats(t, result, tool.OutputStats(""))
}

func TestAFailedExecutedCallReceivesOutputStats(t *testing.T) {
	const output = "permission denied\nexit status 1"
	result := singleResult(t, plainOutputTool(output, errors.New("failed")))

	if !result.Failed {
		t.Error("expected the call to be marked as having failed")
	}
	requireStats(t, result, tool.OutputStats(output))
}

func TestSpecialisedStatsTakePrecedenceOverOutputStats(t *testing.T) {
	want := tool.Stats{Kind: tool.StatsSearch, Lines: 17, Bytes: 1200, TotalBytes: 2400, Truncated: true}
	result := singleResult(t, statsOutputTool("generic output", want))

	requireStats(t, result, want)
}

func TestAnEmptySpecialisedMeasurementFallsBackToOutputStats(t *testing.T) {
	const output = "generic output"
	result := singleResult(t, statsOutputTool(output, tool.Stats{}))

	requireStats(t, result, tool.OutputStats(output))
}

func TestARefusedCallDoesNotReceiveOutputStats(t *testing.T) {
	result := singleResult(t, refusingTool())

	if !result.Failed {
		t.Error("expected the call to be marked as having failed")
	}
	if result.Stats != nil {
		t.Errorf("refused call got stats %#v", result.Stats)
	}
}

type unparsedCallProvider struct {
	name      string
	arguments string
	sent      int
}

func (self *unparsedCallProvider) Configure(string, []tool.Definition) {}
func (self *unparsedCallProvider) AddUserMessage(string)               {}
func (self *unparsedCallProvider) Dump() []json.RawMessage             { return nil }
func (self *unparsedCallProvider) Load([]json.RawMessage)              {}
func (self *unparsedCallProvider) AddToolResults([]agent.ToolResult)   {}

func (self *unparsedCallProvider) Send(_ context.Context, _ agent.Yield) (agent.Reply, error) {
	if self.sent++; self.sent > 1 {
		return agent.Reply{}, nil
	}

	return agent.Reply{Calls: []agent.ToolCall{
		{ID: "a", Name: self.name, Arguments: self.arguments},
	}}, nil
}

func unparsedCallSubject(t *testing.T, tools []tool.Tool, name string, arguments string) string {
	t.Helper()

	provider := &unparsedCallProvider{name: name, arguments: arguments}
	assistant := agent.New("", provider, tools)

	var calls []agent.Event

	for event, err := range assistant.Stream(t.Context(), "go") {
		if err != nil {
			t.Fatal(err)
		}

		if event.Kind == agent.Call {
			calls = append(calls, event)
		}
	}

	if len(calls) != 1 {
		t.Fatalf("expected one call, got %d", len(calls))
	}

	return calls[0].Subject
}

type shoutArgs struct {
	Message string `json:"message"`
	Target  string `json:"target"`
}

func refusingTool() tool.Tool {
	return tool.Implement(
		tool.Definition{
			Name:        "shout",
			Description: "",
			Schema: tool.Schema{
				tool.String("message", "what to shout"),
				tool.String("target", "who to shout at"),
			},
		},
		func(shoutArgs) (string, string) { return "", "" },
	).Validate(func(shoutArgs) error {
		return errors.New("not in the mood")
	}).Plain(func(context.Context, shoutArgs) (string, error) { return "", nil })
}

func TestACallTooMalformedToParseIsStillShownAsItArrived(t *testing.T) {
	got := unparsedCallSubject(t, []tool.Tool{noop()}, "noop", "{not json\nand more besides")

	if got != "{not json" {
		t.Errorf("expected the arguments as they arrived, cut to one line, got %q", got)
	}
}

func TestARefusedCallIsShownAsItsArgumentsInTheOrderTheToolDeclaresThem(t *testing.T) {
	arguments := `{"target":"you","message":"oi"}`
	got := unparsedCallSubject(t, []tool.Tool{refusingTool()}, "shout", arguments)

	if got != "oi you" {
		t.Errorf("expected the values in schema order, got %q", got)
	}
}

func TestARefusedCallWithNothingToShowFallsBackToItsArguments(t *testing.T) {
	arguments := `{"message":"   "}`
	got := unparsedCallSubject(t, []tool.Tool{refusingTool()}, "shout", arguments)

	if got != arguments {
		t.Errorf("expected the raw arguments, got %q", got)
	}
}

type notingProvider struct {
	messages []string
}

func (self *notingProvider) Configure(string, []tool.Definition) {}
func (self *notingProvider) AddUserMessage(text string)          { self.messages = append(self.messages, text) }
func (self *notingProvider) AddToolResults([]agent.ToolResult)   {}

func (self *notingProvider) Send(context.Context, agent.Yield) (agent.Reply, error) {
	return agent.Reply{}, nil
}

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

func TestHowACallLookedIsWrittenFlatSoOldSessionsStillRead(t *testing.T) {
	const stored = `{"kind":"call","id":"1","name":"write","arguments":"{}","render":"a","detail":"1 KB","highlight":{"kind":"syntax","value":"bash"},"read_only":true}`

	const written = `{"render":"a","detail":"1 KB","highlight":{"kind":"syntax","value":"bash"},"read_only":true,"kind":"call","id":"1","name":"write","arguments":"{}"}`

	var event agent.Event
	if err := json.Unmarshal([]byte(stored), &event); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if event.Subject != "a" || event.Note != "1 KB" || !event.ReadOnly {
		t.Errorf("expected a stored appearance to read back, got %#v", event.Rendering)
	}

	got, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(got) != written {
		t.Errorf("expected the event written as one flat object, got %s", got)
	}
}
