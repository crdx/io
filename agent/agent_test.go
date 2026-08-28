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
	"crdx.org/io/internal/stop"
	"crdx.org/io/tool"
)

type callProvider struct {
	sent    int
	results []agent.ToolCallResult
}

func (self *callProvider) Configure(string, []tool.Definition) {}
func (self *callProvider) AddUserMessage(string)               {}

func (self *callProvider) AddToolResults(results []agent.ToolCallResult) {
	self.results = append(self.results, results...)
}

func (self *callProvider) Send(_ context.Context, yield agent.Yield) (agent.Reply, error) {
	self.sent++

	if self.sent > 1 {
		return agent.Reply{}, nil
	}

	yield(agent.Output{Kind: agent.ModelMessageEvent, Text: "thinking out loud"})
	yield(agent.Output{Kind: agent.ModelMessageEvent, Done: true})

	return agent.Reply{Calls: []agent.ToolCall{
		{ID: "a", Name: "noop", Arguments: `{}`},
		{ID: "b", Name: "noop", Arguments: `{}`},
	}}, nil
}

func noop() tool.Tool {
	return tool.Implement(
		tool.Definition{
			Name:        "noop",
			Description: "",
			Schema:      tool.Schema{},
		},
		func(struct{}) (string, string) { return "", "" },
	).IsEmbarrassinglyParallel().Plain(func(context.Context, struct{}) (string, error) {
		return "done", nil
	})
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
		"dropped while streaming text": agent.ModelMessageEvent,
		"dropped on the first call":    agent.ToolCallRequestEvent,
	}

	for name, stopOn := range tests {
		t.Run(name, func(t *testing.T) {
			provider := &callProvider{}
			assistant := agent.New("", provider, []tool.Tool{noop()})

			for update, err := range assistant.Stream(t.Context(), "go") {
				if err != nil {
					t.Fatal(err)
				}

				if update.Event == nil {
					continue
				}

				if update.Event.Kind == stopOn {
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

	for update, err := range assistant.Stream(t.Context(), "go") {
		if err != nil {
			t.Fatal(err)
		}

		if update.Event == nil {
			continue
		}

		if update.Event.Kind == agent.ToolCallResultEvent {
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

	barrierTool := tool.Implement(
		tool.Definition{
			Name:        "noop",
			Description: "",
			Schema:      tool.Schema{},
		},
		func(struct{}) (string, string) { return "", "" },
	).IsEmbarrassinglyParallel().Plain(func(context.Context, struct{}) (string, error) {
		arrivalBarrier.Done()

		select {
		case <-release:
			concurrentCalls.Add(1)
		case <-time.After(time.Second):
		}

		return "done", nil
	})

	provider := &callProvider{}

	if _, err := agent.New("", provider, []tool.Tool{barrierTool}).Send(t.Context(), "go"); err != nil {
		t.Fatal(err)
	}

	if got := concurrentCalls.Load(); got != 2 {
		t.Errorf("expected both calls to be in flight at once, got %d", got)
	}
}

type batchProvider struct {
	calls   int
	sent    int
	results []agent.ToolCallResult
}

func (self *batchProvider) Configure(string, []tool.Definition) {}
func (self *batchProvider) AddUserMessage(string)               {}

func (self *batchProvider) AddToolResults(results []agent.ToolCallResult) {
	self.results = append(self.results, results...)
}

func (self *batchProvider) Send(_ context.Context, _ agent.Yield) (agent.Reply, error) {
	if self.sent++; self.sent > 1 {
		return agent.Reply{}, nil
	}

	calls := make([]agent.ToolCall, self.calls)
	for i := range calls {
		calls[i] = agent.ToolCall{ID: string(rune('a' + i)), Name: "noop", Arguments: `{}`}
	}

	return agent.Reply{Calls: calls}, nil
}

func TestStreamCapsConcurrentCalls(t *testing.T) {
	const concurrencyLimit = 16

	started := make(chan struct{}, concurrencyLimit+1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseCalls := func() {
		releaseOnce.Do(func() { close(release) })
	}
	t.Cleanup(releaseCalls)

	barrierTool := tool.Implement(
		tool.Definition{
			Name:        "noop",
			Description: "",
			Schema:      tool.Schema{},
		},
		func(struct{}) (string, string) { return "", "" },
	).IsEmbarrassinglyParallel().Plain(func(context.Context, struct{}) (string, error) {
		started <- struct{}{}
		<-release
		return "done", nil
	})

	provider := &batchProvider{calls: concurrencyLimit + 1}
	done := make(chan error, 1)
	go func() {
		_, err := agent.New("", provider, []tool.Tool{barrierTool}).Send(t.Context(), "go")
		done <- err
	}()

	for range concurrencyLimit {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for calls to start")
		}
	}

	select {
	case <-started:
		t.Fatal("more calls than the concurrency limit started")
	case <-time.After(20 * time.Millisecond):
	}

	releaseCalls()
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	if got := len(provider.results); got != concurrencyLimit+1 {
		t.Errorf("got %d results, want %d", got, concurrencyLimit+1)
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

	slow := tool.Implement(
		tool.Definition{
			Name:        "noop",
			Description: "",
			Schema:      tool.Schema{},
		},
		func(struct{}) (string, string) { return "", "" },
	).IsEmbarrassinglyParallel().Plain(func(context.Context, struct{}) (string, error) {
		time.Sleep(slept)
		return "done", nil
	})

	assistant := agent.New("", &callProvider{}, []tool.Tool{slow})

	timedResults := 0

	for update, err := range assistant.Stream(t.Context(), "go") {
		if err != nil {
			t.Fatal(err)
		}

		if update.Event == nil {
			continue
		}

		if update.Event.Kind == agent.ToolCallResultEvent {
			timedResults++

			if update.Event.Took < slept {
				t.Errorf("expected the call to have taken at least %s, got %s", slept, update.Event.Took)
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
	call        agent.ToolCall
	sent        int
	definitions []tool.Definition
	results     []agent.ToolCallResult
}

func (self *oneCallProvider) Configure(_ string, definitions []tool.Definition) {
	self.definitions = definitions
}
func (self *oneCallProvider) AddUserMessage(string)   {}
func (self *oneCallProvider) Dump() []json.RawMessage { return nil }
func (self *oneCallProvider) Load([]json.RawMessage)  {}

func (self *oneCallProvider) AddToolResults(results []agent.ToolCallResult) {
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

func TestOnlyEnabledToolsAreOfferedAndCallable(t *testing.T) {
	enabledTool := noop()
	disabledTool := failingTool()
	provider := &oneCallProvider{call: agent.ToolCall{ID: "a", Name: disabledTool.Name(), Arguments: `{}`}}
	assistant := agent.NewWithEnabledTools("", provider, []tool.Tool{enabledTool, disabledTool}, []tool.Tool{enabledTool})

	if len(provider.definitions) != 1 || provider.definitions[0].Name != enabledTool.Name() {
		t.Fatalf("expected only %s to be offered, got %v", enabledTool.Name(), provider.definitions)
	}
	if _, isKnown := assistant.Tool(disabledTool.Name()); !isKnown {
		t.Fatalf("expected %s to remain known", disabledTool.Name())
	}

	for _, err := range assistant.Stream(t.Context(), "go") {
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(provider.results) != 1 || !strings.Contains(provider.results[0].Output, "there is no tool called") {
		t.Errorf("expected the disabled call to be refused, got %v", provider.results)
	}
}

func singleResult(t *testing.T, calledTool tool.Tool) agent.Event {
	t.Helper()

	provider := &oneCallProvider{call: agent.ToolCall{ID: "a", Name: calledTool.Name(), Arguments: `{}`}}
	assistant := agent.New("", provider, []tool.Tool{calledTool})

	var results []agent.Event
	for update, err := range assistant.Stream(t.Context(), "go") {
		if err != nil {
			t.Fatal(err)
		}

		if update.Event == nil {
			continue
		}
		if update.Event.Kind == agent.ToolCallResultEvent {
			results = append(results, *update.Event)
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

	if result.Status != agent.ErrorStatus {
		t.Error("expected the call to be marked as having failed")
	}

	if !strings.Contains(result.Text, "permission denied") {
		t.Errorf("expected what the command printed, got %q", result.Text)
	}
}

func TestAFailedCallIsHandedBackToTheModelAsAFailure(t *testing.T) {
	tests := map[string]struct {
		calledTool  tool.Tool
		wantIsError bool
	}{
		"a call that failed": {calledTool: failingTool(), wantIsError: true},
		"a call that worked": {calledTool: plainOutputTool("done", nil), wantIsError: false},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			provider := &oneCallProvider{
				call: agent.ToolCall{ID: "a", Name: test.calledTool.Name(), Arguments: `{}`},
			}
			assistant := agent.New("", provider, []tool.Tool{test.calledTool})

			for _, err := range assistant.Stream(t.Context(), "go") {
				if err != nil {
					t.Fatal(err)
				}
			}

			if len(provider.results) != 1 {
				t.Fatalf("expected one result, got %d", len(provider.results))
			}

			if provider.results[0].IsError != test.wantIsError {
				t.Errorf("expected IsError %t, got %t", test.wantIsError, provider.results[0].IsError)
			}
		})
	}
}

func TestASuccessfulCallIsMarkedSuccessful(t *testing.T) {
	result := singleResult(t, plainOutputTool("done", nil))

	if result.Status != agent.SuccessStatus {
		t.Errorf("got status %q", result.Status)
	}
}

func TestAReadOnlyCallStoresReadOnlyFallbackRendering(t *testing.T) {
	calledTool := tool.Implement(
		tool.Definition{Name: "inspect", Description: "", Schema: tool.Schema{}},
		func(struct{}) (string, string) { return "target", "" },
	).ChangesNothing().Plain(func(context.Context, struct{}) (string, error) {
		return "done", nil
	})
	provider := &oneCallProvider{
		call: agent.ToolCall{ID: "a", Name: calledTool.Name(), Arguments: `{}`},
	}
	assistant := agent.New("", provider, []tool.Tool{calledTool})

	for update, err := range assistant.Stream(t.Context(), "go") {
		if err != nil {
			t.Fatal(err)
		}
		if update.Event != nil && update.Event.Kind == agent.ToolCallRequestEvent {
			if !update.Event.ReadOnly {
				t.Error("read-only call did not store read-only fallback rendering")
			}
			return
		}
	}

	t.Fatal("expected a tool call request")
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

func TestACallTheUserStoppedIsMarkedApartFromOneThatFailed(t *testing.T) {
	ctx, cancel := context.WithCancelCause(t.Context())
	defer cancel(nil)

	stoppedTool := tool.Implement(
		tool.Definition{Name: "output", Description: "", Schema: tool.Schema{}},
		func(struct{}) (string, string) { return "", "" },
	).Plain(func(callContext context.Context, _ struct{}) (string, error) {
		cancel(stop.Because("the user pressed escape"))
		return "half done", callContext.Err()
	})

	provider := &oneCallProvider{call: agent.ToolCall{ID: "a", Name: "output", Arguments: `{}`}}
	assistant := agent.New("", provider, []tool.Tool{stoppedTool})

	var result *agent.Event
	for update := range assistant.Stream(ctx, "go") {
		if update.Event != nil && update.Event.Kind == agent.ToolCallResultEvent {
			result = update.Event
		}
	}

	if result == nil {
		t.Fatal("expected the stopped call to report a result")
	}
	if result.Status != agent.CancelledStatus {
		t.Errorf("got status %q, want the call marked as stopped rather than failed", result.Status)
	}
}

func TestAFailedExecutedCallReceivesOutputStats(t *testing.T) {
	const output = "permission denied\nexit status 1"
	result := singleResult(t, plainOutputTool(output, errors.New("failed")))

	if result.Status != agent.ErrorStatus {
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

	if result.Status != agent.ErrorStatus {
		t.Error("expected the call to be marked as having failed")
	}
	if result.Stats != nil {
		t.Errorf("refused call got stats %#v", result.Stats)
	}
}

type unparsedToolCallProvider struct {
	name      string
	arguments string
	sent      int
}

func (self *unparsedToolCallProvider) Configure(string, []tool.Definition)   {}
func (self *unparsedToolCallProvider) AddUserMessage(string)                 {}
func (self *unparsedToolCallProvider) Dump() []json.RawMessage               { return nil }
func (self *unparsedToolCallProvider) Load([]json.RawMessage)                {}
func (self *unparsedToolCallProvider) AddToolResults([]agent.ToolCallResult) {}

func (self *unparsedToolCallProvider) Send(_ context.Context, _ agent.Yield) (agent.Reply, error) {
	if self.sent++; self.sent > 1 {
		return agent.Reply{}, nil
	}

	return agent.Reply{Calls: []agent.ToolCall{
		{ID: "a", Name: self.name, Arguments: self.arguments},
	}}, nil
}

func unparsedToolCallSubject(t *testing.T, tools []tool.Tool, name string, arguments string) string {
	t.Helper()

	provider := &unparsedToolCallProvider{name: name, arguments: arguments}
	assistant := agent.New("", provider, tools)

	var calls []agent.Event

	for update, err := range assistant.Stream(t.Context(), "go") {
		if err != nil {
			t.Fatal(err)
		}

		if update.Event == nil {
			continue
		}

		if update.Event.Kind == agent.ToolCallRequestEvent {
			calls = append(calls, *update.Event)
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
	got := unparsedToolCallSubject(t, []tool.Tool{noop()}, "noop", "{not json\nand more besides")

	if got != "{not json" {
		t.Errorf("expected the arguments as they arrived, cut to one line, got %q", got)
	}
}

func TestARefusedCallIsShownAsItsArgumentsInTheOrderTheToolDeclaresThem(t *testing.T) {
	arguments := `{"target":"you","message":"oi"}`
	got := unparsedToolCallSubject(t, []tool.Tool{refusingTool()}, "shout", arguments)

	if got != "oi you" {
		t.Errorf("expected the values in schema order, got %q", got)
	}
}

func TestARefusedCallWithOnlyBlankArgumentsHasNoSubject(t *testing.T) {
	arguments := `{"message":"   "}`
	got := unparsedToolCallSubject(t, []tool.Tool{refusingTool()}, "shout", arguments)

	if got != "" {
		t.Errorf("expected no subject, got %q", got)
	}
}

type notingProvider struct {
	messages []string
}

func (self *notingProvider) Configure(string, []tool.Definition)   {}
func (self *notingProvider) AddUserMessage(text string)            { self.messages = append(self.messages, text) }
func (self *notingProvider) AddToolResults([]agent.ToolCallResult) {}

func (self *notingProvider) Send(context.Context, agent.Yield) (agent.Reply, error) {
	return agent.Reply{}, nil
}

func TestANoteGoesAheadOfTheNextPrompt(t *testing.T) {
	provider := &notingProvider{}
	self := agent.New("", provider, nil)

	self.AddUserMessage("something changed")

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
	const stored = `{"kind":"call","id":"1","name":"write","arguments":"{}","render":"a","detail":"1 KB","emphasis":{"kind":"syntax","value":"bash"},"read_only":true}`

	const written = `{"render":"a","detail":"1 KB","emphasis":{"kind":"syntax","value":"bash"},"read_only":true,"kind":"call","id":"1","name":"write","arguments":"{}"}`

	var event agent.Event
	if err := json.Unmarshal([]byte(stored), &event); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if event.Subject != "a" || event.Note != "1 KB" || !event.ReadOnly {
		t.Errorf("expected a stored appearance to read back, got %#v", event.FallbackRendering)
	}

	got, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(got) != written {
		t.Errorf("expected the event written as one flat object, got %s", got)
	}
}

type outputProvider struct {
	outputs []agent.Output
	reply   agent.Reply
	err     error
}

func (*outputProvider) Configure(string, []tool.Definition)   {}
func (*outputProvider) AddUserMessage(string)                 {}
func (*outputProvider) AddToolResults([]agent.ToolCallResult) {}
func (self *outputProvider) Send(_ context.Context, yield agent.Yield) (agent.Reply, error) {
	for _, output := range self.outputs {
		if !yield(output) {
			return agent.Reply{}, nil
		}
	}

	return self.reply, self.err
}

func TestStreamAttachesUsageToOneExistingResponseEvent(t *testing.T) {
	usage := agent.Usage{InputTokens: 5000}
	provider := &outputProvider{
		outputs: []agent.Output{
			{Kind: agent.ModelReasoningEvent, Text: "thought"},
			{Kind: agent.ModelReasoningEvent, Done: true, Usage: &usage},
			{Kind: agent.ModelMessageEvent, Text: "answer"},
			{Kind: agent.ModelMessageEvent, Done: true, Usage: &usage},
		},
		reply: agent.Reply{Usage: usage},
	}
	assistant := agent.New("", provider, nil)

	var reported []agent.Event
	for update, err := range assistant.Stream(t.Context(), "go") {
		if err != nil {
			t.Fatal(err)
		}
		if update.Event != nil && update.Event.Usage != nil {
			reported = append(reported, *update.Event)
		}
	}

	if len(reported) != 1 || reported[0].Kind != agent.ModelReasoningEvent ||
		reported[0].Usage.InputTokens != 5000 {
		t.Errorf("got usage on %+v", reported)
	}
}

func TestStreamSeparatesCompletedProseBlocksFromDeltas(t *testing.T) {
	provider := &outputProvider{outputs: []agent.Output{
		{Kind: agent.ModelReasoningEvent, Text: "first "},
		{Kind: agent.ModelReasoningEvent, Text: "thought"},
		{Kind: agent.ModelReasoningEvent, Done: true},
		{Kind: agent.ModelReasoningEvent, Text: "second thought"},
		{Kind: agent.ModelReasoningEvent, Done: true},
		{Kind: agent.ModelMessageEvent, Text: "answer"},
		{Kind: agent.ModelMessageEvent, Done: true},
	}}
	assistant := agent.New("", provider, nil)

	var got []string
	for update, err := range assistant.Stream(t.Context(), "go") {
		if err != nil {
			t.Fatal(err)
		}

		switch {
		case update.Delta != nil:
			got = append(got, "delta:"+string(update.Delta.Kind)+":"+update.Delta.Text)
		case update.Event != nil:
			got = append(got, "event:"+string(update.Event.Kind)+":"+update.Event.Text)
		}
	}

	want := []string{
		"event:user_message:go",
		"delta:model_reasoning:first ",
		"delta:model_reasoning:thought",
		"event:model_reasoning:first thought",
		"delta:model_reasoning:second thought",
		"event:model_reasoning:second thought",
		"delta:model_message:answer",
		"event:model_message:answer",
	}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestStreamDiscardsIncompleteReasoning(t *testing.T) {
	failure := errors.New("stream failed")
	provider := &outputProvider{
		outputs: []agent.Output{{Kind: agent.ModelReasoningEvent, Text: "half a thought"}},
		err:     failure,
	}
	assistant := agent.New("", provider, nil)

	var completedReasoning bool
	for update, err := range assistant.Stream(t.Context(), "go") {
		if err != nil {
			if !errors.Is(err, failure) {
				t.Fatalf("got %v, want %v", err, failure)
			}
			continue
		}
		completedReasoning = completedReasoning || update.Event != nil && update.Event.Kind == agent.ModelReasoningEvent
	}

	if completedReasoning {
		t.Error("incomplete reasoning became a durable event")
	}
}

func TestStreamDropsIncompleteProseWhenItsKindChanges(t *testing.T) {
	provider := &outputProvider{outputs: []agent.Output{
		{Kind: agent.ModelReasoningEvent, Text: "unfinished thought"},
		{Kind: agent.ModelMessageEvent, Text: "answer"},
		{Kind: agent.ModelMessageEvent, Done: true},
	}}
	assistant := agent.New("", provider, nil)

	var completed []agent.Event
	for update, err := range assistant.Stream(t.Context(), "go") {
		if err != nil {
			t.Fatal(err)
		}
		if update.Event != nil && (update.Event.Kind == agent.ModelReasoningEvent || update.Event.Kind == agent.ModelMessageEvent) {
			completed = append(completed, *update.Event)
		}
	}

	if len(completed) != 1 || completed[0].Kind != agent.ModelMessageEvent || completed[0].Text != "answer" {
		t.Errorf("got completed prose %+v", completed)
	}
}

func TestStreamPreservesAnIncompleteAnswerBeforeTheFailure(t *testing.T) {
	failure := errors.New("stream failed")
	provider := &outputProvider{
		outputs: []agent.Output{{Kind: agent.ModelMessageEvent, Text: "half an answer"}},
		err:     failure,
	}
	assistant := agent.New("", provider, nil)

	var answer string
	var reportedFailure error
	for update, err := range assistant.Stream(t.Context(), "go") {
		if err != nil {
			reportedFailure = err
			continue
		}
		if update.Event != nil && update.Event.Kind == agent.ModelMessageEvent {
			answer = update.Event.Text
		}
	}

	if answer != "half an answer" {
		t.Errorf("got answer %q", answer)
	}
	if !errors.Is(reportedFailure, failure) {
		t.Errorf("got failure %v, want %v", reportedFailure, failure)
	}
}
