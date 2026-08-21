// Package agent runs a conversation between a model and a set of tools.
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"strings"
	"time"

	"crdx.org/io/internal/strutil"
	"crdx.org/io/tool"
)

// New builds an agent that talks to a provider with a set of tools on offer.
func New(prompt string, provider Provider, tools []tool.Tool) *Agent {
	definitions := make([]tool.Definition, len(tools))
	availableTools := make(map[string]tool.Tool, len(tools))
	stateOwners := map[string]tool.Tool{}

	for i, offeredTool := range tools {
		definitions[i] = tool.Describe(offeredTool)
		availableTools[offeredTool.Name()] = offeredTool
		if stateKey := offeredTool.StateKey(); stateKey != "" {
			stateOwners[stateKey] = offeredTool
		}
	}

	provider.Configure(prompt, definitions)

	return &Agent{provider: provider, tools: availableTools, stateOwners: stateOwners}
}

// Dump carries out the provider's append-only conversation state.
func (self *Agent) Dump() ([]json.RawMessage, error) {
	state, ok := self.provider.(State)
	if !ok {
		return nil, ErrNoState
	}

	items := state.Dump()
	if len(items) < len(self.state) {
		return nil, ErrStateReplaced
	}
	for i := range self.state {
		if !bytes.Equal(items[i], self.state[i]) {
			return nil, ErrStateReplaced
		}
	}

	self.state = cloneState(items)
	return cloneState(items), nil
}

// Load replaces the provider's conversation state with items carried in earlier.
func (self *Agent) Load(items []json.RawMessage) error {
	state, ok := self.provider.(State)
	if !ok {
		return ErrNoState
	}
	state.Load(cloneState(items))
	self.state = cloneState(items)
	return nil
}

func cloneState(items []json.RawMessage) []json.RawMessage {
	clonedItems := make([]json.RawMessage, len(items))
	for i, item := range items {
		clonedItems[i] = bytes.Clone(item)
	}
	return clonedItems
}

// Note adds something for the model to read before the next thing it is asked.
func (self *Agent) Note(text string) {
	self.provider.AddUserMessage(text)
}

// Stream yields a prompt and every event through its final tool round.
func (self *Agent) Stream(ctx context.Context, prompt string) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		self.provider.AddUserMessage(prompt)

		if !yield(Event{Kind: Prompt, Text: prompt}, nil) {
			return
		}

		for {
			listening := true

			reply, err := self.provider.Send(ctx, func(event Event) bool {
				listening = yield(event, nil)
				return listening
			})

			switch {
			case !listening:
				self.answer(cancelledResults(reply.Calls))
				return
			case err != nil:
				yield(Event{}, err)
				return
			}

			if len(reply.Calls) == 0 {
				return
			}

			if !self.runCalls(ctx, reply.Calls, yield) {
				return
			}
		}
	}
}

// Send answers one prompt the whole way through, and returns everything the model said.
func (self *Agent) Send(ctx context.Context, prompt string) (string, error) {
	var answer strings.Builder
	var failure error

	for event, err := range self.Stream(ctx, prompt) {
		if err != nil {
			failure = err
			break
		}

		if event.Kind == Text {
			answer.WriteString(event.Text)
		}
	}

	return answer.String(), failure
}

// CancelledOutput is what a call that never ran hands back.
const CancelledOutput = "the call was cancelled"

func cancelledResults(calls []ToolCall) []ToolResult {
	results := make([]ToolResult, len(calls))

	for i, call := range calls {
		results[i] = ToolResult{ID: call.ID, Output: CancelledOutput}
	}

	return results
}

func (self *Agent) answer(results []ToolResult) {
	if len(results) > 0 {
		self.provider.AddToolResults(results)
	}
}

type pendingCall struct {
	rawCall    ToolCall  // the call as received
	parsedCall tool.Call // the call ready to run
	failure    string
}

func (self *Agent) runCalls(
	ctx context.Context,
	calls []ToolCall,
	yield func(Event, error) bool,
) bool {
	results := cancelledResults(calls)

	defer func() { self.answer(results) }()

	queuedCalls := make([]pendingCall, len(calls))

	for i, rawCall := range calls {
		parsedCall, failure := self.parseCall(rawCall)
		queuedCalls[i] = pendingCall{
			rawCall:    rawCall,
			parsedCall: parsedCall,
			failure:    failure,
		}

		event := Event{
			Kind:      Call,
			ID:        rawCall.ID,
			Arguments: rawCall.Arguments,
			Name:      rawCall.Name,
			Rendering: Rendering{
				ReadOnly: self.readOnly(rawCall),
			},
		}

		if parsedCall != nil {
			event.Describe(parsedCall)
		} else {
			event.Subject = self.describeUnparsedCall(rawCall)
		}

		if !yield(event, nil) {
			return false
		}
	}

	listening := true

	for start := 0; start < len(queuedCalls) && listening; {
		end := self.batchEnd(queuedCalls, start)
		listening = self.runBatch(ctx, queuedCalls[start:end], results[start:end], yield)
		start = end
	}

	return listening
}

func (self *Agent) batchEnd(queuedCalls []pendingCall, start int) int {
	if !self.concurrent(queuedCalls[start].rawCall) {
		return start + 1
	}

	end := start + 1
	for end < len(queuedCalls) && self.concurrent(queuedCalls[end].rawCall) {
		end++
	}

	return end
}

func (self *Agent) concurrent(call ToolCall) bool {
	calledTool, found := self.tools[call.Name]

	return found && calledTool.Concurrent()
}

func (self *Agent) describeUnparsedCall(call ToolCall) string {
	calledTool, found := self.tools[call.Name]
	if !found {
		return strutil.FirstLine(call.Arguments)
	}

	return tool.DescribeUnparsedArguments(calledTool, call.Arguments)
}

func (self *Agent) readOnly(call ToolCall) bool {
	calledTool, found := self.tools[call.Name]

	return found && calledTool.ReadOnly()
}

type completedCall struct {
	result Event
	state  Event
}

func (self *Agent) runBatch(
	ctx context.Context,
	batch []pendingCall, results []ToolResult,
	yield func(Event, error) bool,
) bool {
	done := make(chan completedCall, len(batch))

	for i, item := range batch {
		go func() {
			startedAt := time.Now()

			executionResult := tool.Result{Output: item.failure}
			ok := false
			if item.parsedCall != nil {
				executionResult, ok = exec(ctx, item.parsedCall)
			}

			results[i] = ToolResult{
				ID:     item.rawCall.ID,
				Output: executionResult.Output,
				Image:  executionResult.Image,
			}

			if item.parsedCall != nil && executionResult.Stats.Kind == "" {
				executionResult.Stats = tool.OutputStats(executionResult.Output)
			}

			var stats *tool.Stats
			if executionResult.Stats.Kind != "" {
				stats = &executionResult.Stats
			}

			completion := completedCall{result: Event{
				Kind:   Result,
				ID:     item.rawCall.ID,
				Name:   item.rawCall.Name,
				Text:   executionResult.Output,
				Failed: !ok,
				Took:   time.Since(startedAt),
				Stats:  stats,
			}}
			if ok && len(executionResult.State) > 0 {
				completion.state = Event{
					Kind:  StateEvent,
					ID:    item.rawCall.ID,
					Name:  self.tools[item.rawCall.Name].StateKey(),
					State: executionResult.State,
				}
			}

			done <- completion
		}()
	}

	listening := true

	for range batch {
		completion := <-done

		if completion.state.Kind != "" {
			if err := self.restoreState(completion.state); err != nil {
				if listening {
					yield(Event{}, err)
				}
				return false
			}
		}
		if listening && completion.state.Kind != "" {
			listening = yield(completion.state, nil)
		}
		if listening {
			listening = yield(completion.result, nil)
		}
	}

	return listening
}

// RestoreState replays durable state transitions owned by the available tools.
func (self *Agent) RestoreState(events []Event) error {
	for _, event := range events {
		if event.Kind != StateEvent {
			continue
		}
		if err := self.restoreState(event); err != nil {
			return err
		}
	}

	return nil
}

func (self *Agent) restoreState(event Event) error {
	calledTool, known := self.stateOwners[event.Name]
	if !known {
		return nil
	}
	if err := calledTool.Restore(event.State); err != nil {
		return fmt.Errorf("could not restore %s state: %w", event.Name, err)
	}

	return nil
}

// Tool is the tool of that name, and whether there is one.
func (self *Agent) Tool(name string) (tool.Tool, bool) {
	found, known := self.tools[name]
	return found, known
}

func (self *Agent) parseCall(call ToolCall) (tool.Call, string) {
	calledTool, found := self.tools[call.Name]
	if !found {
		return nil, fmt.Sprintf("there is no tool called %q", call.Name)
	}

	parsedCall, err := calledTool.Parse(call.Arguments)
	if err != nil {
		return nil, err.Error()
	}

	return parsedCall, ""
}

func exec(ctx context.Context, call tool.Call) (tool.Result, bool) {
	result, err := call.Exec(ctx)

	switch {
	case err == nil:
		return result, true
	case result.Output != "":
		return result, false
	}

	result.Output = err.Error()
	return result, false
}
