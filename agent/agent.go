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

	"crdx.org/io/tool"
)

// New builds an agent that talks to a provider with a set of tools on offer.
func New(prompt string, provider Provider, tools []tool.Tool) *Agent {
	definitions := make([]tool.Definition, len(tools))
	availableTools := make(map[string]tool.Tool, len(tools))

	for index, offeredTool := range tools {
		definitions[index] = tool.Describe(offeredTool)
		availableTools[offeredTool.Name()] = offeredTool
	}

	provider.Configure(prompt, definitions)

	return &Agent{provider: provider, tools: availableTools}
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
	for index := range self.state {
		if !bytes.Equal(items[index], self.state[index]) {
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
	for index, item := range items {
		clonedItems[index] = bytes.Clone(item)
	}
	return clonedItems
}

// Note adds something for the model to read before the next thing it is asked, without asking it
// anything. Nothing is sent by this: it goes out with whatever turn comes next.
func (self *Agent) Note(text string) {
	self.provider.AddUserMessage(text)
}

// Stream yields a prompt and every event through its final tool round. Context cancellation stops
// provider requests but waits for tools already running.
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
			case len(reply.Calls) == 0:
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

	for index, call := range calls {
		results[index] = ToolResult{ID: call.ID, Output: CancelledOutput}
	}

	return results
}

func (self *Agent) answer(results []ToolResult) {
	if len(results) > 0 {
		self.provider.AddToolResults(results)
	}
}

type pendingCall struct {
	call       ToolCall  // the call as received
	parsedCall tool.Call // the call ready to run
	failure    string    // why parsing failed
}

func (self *Agent) runCalls(
	ctx context.Context,
	calls []ToolCall,
	yield func(Event, error) bool,
) bool {
	results := cancelledResults(calls)

	defer func() { self.answer(results) }()

	queuedCalls := make([]pendingCall, len(calls))

	for index, rawCall := range calls {
		parsedCall, failure := self.parseCall(rawCall)
		queuedCalls[index] = pendingCall{call: rawCall, parsedCall: parsedCall, failure: failure}

		event := Event{
			Kind:      Call,
			Name:      rawCall.Name,
			Arguments: rawCall.Arguments,
			ID:        rawCall.ID,
			ReadOnly:  self.readOnly(rawCall),
		}

		if parsedCall != nil {
			event.Render = parsedCall.Render()
			event.Detail = parsedCall.Detail()

			if highlightedCall, ok := parsedCall.(tool.HighlightedCall); ok {
				event.Highlight = highlightedCall.Highlight()
			}
		}

		if !yield(event, nil) {
			return false
		}
	}

	listening := true

	for start := 0; start < len(queuedCalls) && listening; {
		end := self.batchEnd(queuedCalls, start)
		listening = runBatch(ctx, queuedCalls[start:end], results[start:end], yield)
		start = end
	}

	return listening
}

func (self *Agent) batchEnd(queuedCalls []pendingCall, start int) int {
	if !self.concurrent(queuedCalls[start].call) {
		return start + 1
	}

	end := start + 1
	for end < len(queuedCalls) && self.concurrent(queuedCalls[end].call) {
		end++
	}

	return end
}

func (self *Agent) concurrent(call ToolCall) bool {
	calledTool, found := self.tools[call.Name]

	return found && calledTool.Concurrent()
}

func (self *Agent) readOnly(call ToolCall) bool {
	calledTool, found := self.tools[call.Name]

	return found && calledTool.ReadOnly()
}

func runBatch(
	ctx context.Context,
	batch []pendingCall, results []ToolResult,
	yield func(Event, error) bool,
) bool {
	done := make(chan Event, len(batch))

	for index, item := range batch {
		go func() {
			startedAt := time.Now()

			payload, ok := item.failure, false
			if item.parsedCall != nil {
				payload, ok = exec(ctx, item.parsedCall)
			}

			result := ToolResult{ID: item.call.ID, Output: payload}
			if image, attached := tool.AttachedImage(item.parsedCall); attached {
				result.Image = image
			}
			results[index] = result

			var stats *tool.Statistics
			if measuredStats, ok := tool.Stats(item.parsedCall); ok {
				stats = &measuredStats
			}

			done <- Event{
				Kind:       Result,
				ID:         item.call.ID,
				Name:       item.call.Name,
				Text:       payload,
				Failed:     !ok,
				Took:       time.Since(startedAt),
				Statistics: stats,
			}
		}()
	}

	listening := true

	for range batch {
		event := <-done

		if listening {
			listening = yield(event, nil)
		}
	}

	return listening
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

func exec(ctx context.Context, call tool.Call) (string, bool) {
	output, err := call.Exec(ctx)

	switch {
	case err == nil:
		return output, true
	case output != "":
		return output, false // a call that failed with something to say says it, not only why
	}

	return err.Error(), false
}
