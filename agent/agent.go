package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"strings"
	"time"

	"crdx.org/io/internal/stop"
	"crdx.org/io/internal/util/strutil"
	"crdx.org/io/tool"
)

func New(systemPrompt string, provider Provider, tools []tool.Tool) *Agent {
	return NewWithEnabledTools(systemPrompt, provider, tools, tools)
}

func NewWithEnabledTools(systemPrompt string, provider Provider, tools []tool.Tool, enabledTools []tool.Tool) *Agent {
	definitions := make([]tool.Definition, len(enabledTools))
	enabledNames := make(map[string]struct{}, len(enabledTools))
	for i, enabledTool := range enabledTools {
		definitions[i] = tool.Describe(enabledTool)
		enabledNames[enabledTool.Name()] = struct{}{}
	}

	availableTools := make(map[string]tool.Tool, len(tools))
	stateOwners := map[string]tool.Tool{}
	for _, availableTool := range tools {
		availableTools[availableTool.Name()] = availableTool
		if stateKey := availableTool.StateKey(); stateKey != "" {
			stateOwners[stateKey] = availableTool
		}
	}

	provider.Configure(systemPrompt, definitions)

	return &Agent{provider: provider, registeredTools: availableTools, enabledToolNames: enabledNames, owners: stateOwners}
}

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

func (self *Agent) AddUserMessage(text string) {
	self.provider.AddUserMessage(text)
}

type proseStream struct {
	kind             Kind
	text             strings.Builder
	pending          *Event
	hasReportedUsage bool
}

func (self *proseStream) add(output Output) []Update {
	output.Text = strutil.StripControl(output.Text)

	if output.Done {
		if self.kind != output.Kind || self.text.Len() == 0 {
			self.resetText()
			return nil
		}

		event := Event{Kind: self.kind, Text: self.text.String()}
		if output.Usage != nil && !self.hasReportedUsage {
			event.Usage = output.Usage
			self.hasReportedUsage = true
		}
		self.resetText()
		if !output.AwaitUsage {
			return append(self.takePending(), Update{Event: &event})
		}

		self.pending = &event
		return nil
	}

	if output.Text == "" {
		return nil
	}

	updates := self.takePending()

	if self.kind != "" && self.kind != output.Kind {
		self.resetText()
	}

	self.kind = output.Kind
	self.text.WriteString(output.Text)
	delta := Delta{Kind: output.Kind, Text: output.Text}

	return append(updates, Update{Delta: &delta})
}

func (self *proseStream) finish(usage Usage) []Update {
	if self.pending != nil && usage.InputTokens > 0 && !self.hasReportedUsage {
		self.pending.Usage = &usage
		self.hasReportedUsage = true
	}

	return self.takePending()
}

func (self *proseStream) interrupted() []Update {
	updates := self.takePending()
	if self.kind == ModelMessageEvent && self.text.Len() > 0 {
		event := Event{Kind: self.kind, Text: self.text.String()}
		updates = append(updates, Update{Event: &event})
	}
	self.resetText()

	return updates
}

func (self *proseStream) takePending() []Update {
	if self.pending == nil {
		return nil
	}

	update := Update{Event: self.pending}
	self.pending = nil
	return []Update{update}
}

func (self *proseStream) resetText() {
	self.kind = ""
	self.text.Reset()
}

func (self *Agent) Stream(ctx context.Context, message string) iter.Seq2[Update, error] {
	return func(yield func(Update, error) bool) {
		yieldEvent := func(event Event, err error) bool {
			update := Update{}
			if err == nil {
				update.Event = &event
			}

			return yield(update, err)
		}
		yieldUpdates := func(updates []Update) bool {
			for _, update := range updates {
				if !yield(update, nil) {
					return false
				}
			}

			return true
		}

		self.provider.AddUserMessage(message)

		if !yieldEvent(Event{Kind: UserMessageEvent, Text: message}, nil) {
			return
		}

		for {
			var prose proseStream

			reply, listening, err := self.send(ctx, &prose, yieldUpdates, yieldEvent)

			switch {
			case !listening:
				self.answer(cancelledResults(ctx, reply.Calls))
				return
			case err != nil:
				if !yieldUpdates(prose.interrupted()) {
					return
				}
				yield(Update{}, err)
				return
			case len(reply.Calls) == 0:
				yieldUpdates(prose.finish(reply.Usage))
				return
			}

			if !yieldUpdates(prose.finish(Usage{})) {
				self.answer(cancelledResults(ctx, reply.Calls))
				return
			}

			usage := reply.Usage
			if prose.hasReportedUsage {
				usage = Usage{}
			}
			if !self.runCalls(ctx, reply.Calls, usage, yieldEvent) {
				return
			}
		}
	}
}

func (self *Agent) send(
	ctx context.Context,
	prose *proseStream,
	yieldUpdates func([]Update) bool,
	yieldEvent func(Event, error) bool,
) (Reply, bool, error) {
	isListening := true
	askedRewind := rewindOf(self.provider)

	var spentTime time.Duration

	for attempt := 1; ; attempt++ {
		reply, err := self.provider.Send(ctx, func(output Output) bool {
			isListening = yieldUpdates(prose.add(output))
			return isListening
		})

		if !isListening || err == nil {
			return reply, isListening, err
		}

		wait, worthIt := self.retryWait(err, attempt, spentTime)
		if !worthIt {
			return reply, isListening, err
		}

		spentTime += wait

		if !isResumable(err) {
			askedRewind.restore()
		}

		if !yieldUpdates(prose.interrupted()) {
			return reply, false, err
		}

		notice := Event{
			Kind:    RetryingEvent,
			Text:    err.Error(),
			Attempt: attempt,
			Took:    wait,
		}

		if call, faultedCall := faultedCall(err); faultedCall {
			notice.ID, notice.Name, notice.Arguments = call.ID, call.Name, call.Arguments
		}

		if !yieldEvent(notice, nil) {
			return reply, false, err
		}

		if !self.waitBeforeRetry(ctx, wait) {
			return reply, isListening, err
		}
	}
}

func (self *Agent) Send(ctx context.Context, message string) (string, error) {
	var answer strings.Builder
	var failure error

	for update, err := range self.Stream(ctx, message) {
		if err != nil {
			failure = err
			break
		}

		if update.Event != nil && update.Event.Kind == ModelMessageEvent {
			answer.WriteString(update.Event.Text)
		}
	}

	return answer.String(), failure
}

const CancelledOutput = "the call was cancelled"

func resultStatus(ctx context.Context, ok bool) Status {
	switch {
	case ok:
		return SuccessStatus
	case ctx.Err() != nil:
		return CancelledStatus
	default:
		return ErrorStatus
	}
}

func cancelledResults(ctx context.Context, calls []ToolCall) []ToolCallResult {
	results := make([]ToolCallResult, len(calls))
	output := CancelledOutput + stop.Phrase(ctx)

	for i, call := range calls {
		results[i] = ToolCallResult{ID: call.ID, Output: output, IsError: true}
	}

	return results
}

func (self *Agent) answer(results []ToolCallResult) {
	if len(results) > 0 {
		self.provider.AddToolResults(results)
	}
}

type pendingCall struct {
	rawToolCall    ToolCall
	parsedToolCall tool.ToolCall

	err string
}

func (self *Agent) runCalls(
	ctx context.Context,
	calls []ToolCall,
	usage Usage,
	yield func(Event, error) bool,
) bool {
	results := cancelledResults(ctx, calls)

	defer func() { self.answer(results) }()

	queuedCalls := make([]pendingCall, len(calls))

	for i, rawCall := range calls {
		parsedToolCall, err := self.parseCall(rawCall)
		queuedCalls[i] = pendingCall{
			rawToolCall:    rawCall,
			parsedToolCall: parsedToolCall,
			err:            err,
		}

		event := Event{
			Kind:      ToolCallRequestEvent,
			ID:        rawCall.ID,
			Arguments: rawCall.Arguments,
			Name:      rawCall.Name,
			FallbackRendering: FallbackRendering{
				ReadOnly: self.readOnly(rawCall),
			},
		}
		if i == len(calls)-1 && usage.InputTokens > 0 {
			event.Usage = &usage
		}

		if parsedToolCall != nil {
			event.Describe(parsedToolCall)
		} else {
			event.Subject = self.describeUnparsedToolCall(rawCall)
		}

		if !yield(event, nil) {
			return false
		}
	}

	isListening := true

	for start := 0; start < len(queuedCalls) && isListening; {
		end := self.batchEnd(queuedCalls, start)
		isListening = self.runBatch(ctx, queuedCalls[start:end], results[start:end], yield)
		start = end
	}

	return isListening
}

func (self *Agent) batchEnd(queuedCalls []pendingCall, start int) int {
	if !self.concurrent(queuedCalls[start].rawToolCall) {
		return start + 1
	}

	end := start + 1
	for end < len(queuedCalls) && self.concurrent(queuedCalls[end].rawToolCall) {
		end++
	}

	return end
}

func (self *Agent) concurrent(call ToolCall) bool {
	calledTool, isFound := self.registeredTools[call.Name]

	return isFound && calledTool.Concurrent()
}

func (self *Agent) describeUnparsedToolCall(call ToolCall) string {
	calledTool, isFound := self.registeredTools[call.Name]
	if !isFound {
		return strutil.FirstLine(call.Arguments)
	}

	return tool.DescribeUnparsedArguments(calledTool, call.Arguments)
}

func (self *Agent) readOnly(call ToolCall) bool {
	calledTool, isFound := self.registeredTools[call.Name]

	return isFound && calledTool.ReadOnly()
}

type completedToolCall struct {
	result Event
	state  Event
}

const maxConcurrentToolCalls = 16

func (self *Agent) runBatch(
	ctx context.Context,
	batch []pendingCall, results []ToolCallResult,
	yield func(Event, error) bool,
) bool {
	done := make(chan completedToolCall, len(batch))
	availableSlots := make(chan struct{}, maxConcurrentToolCalls)

	for i, item := range batch {
		availableSlots <- struct{}{}
		go func() {
			defer func() { <-availableSlots }()
			startedAt := time.Now()

			executionResult := tool.ToolCallResult{Output: item.err}
			ok := false
			if item.parsedToolCall != nil {
				executionResult, ok = exec(ctx, item.parsedToolCall)
			}

			results[i] = ToolCallResult{
				ID:      item.rawToolCall.ID,
				Output:  executionResult.Output,
				Image:   executionResult.Image,
				IsError: !ok,
			}

			if item.parsedToolCall != nil && executionResult.Stats.Kind == "" {
				executionResult.Stats = tool.OutputStats(executionResult.Output)
			}

			var stats *tool.Stats
			if executionResult.Stats.Kind != "" {
				stats = &executionResult.Stats
			}

			status := resultStatus(ctx, ok)

			completion := completedToolCall{result: Event{
				Kind:   ToolCallResultEvent,
				ID:     item.rawToolCall.ID,
				Name:   item.rawToolCall.Name,
				Text:   executionResult.Output,
				Status: status,
				Took:   time.Since(startedAt),
				Stats:  stats,
			}}
			if ok && len(executionResult.State) > 0 {
				completion.state = Event{
					Kind:  StateChangeEvent,
					ID:    item.rawToolCall.ID,
					Name:  self.registeredTools[item.rawToolCall.Name].StateKey(),
					State: executionResult.State,
				}
			}

			done <- completion
		}()
	}

	isListening := true

	for range batch {
		completion := <-done

		if completion.state.Kind != "" {
			if err := self.restoreState(completion.state); err != nil {
				if isListening {
					yield(Event{}, err)
				}
				return false
			}
		}
		if isListening && completion.state.Kind != "" {
			isListening = yield(completion.state, nil)
		}
		if isListening {
			isListening = yield(completion.result, nil)
		}
	}

	return isListening
}

func (self *Agent) RestoreState(events []Event) error {
	for _, event := range events {
		if event.Kind != StateChangeEvent {
			continue
		}
		if err := self.restoreState(event); err != nil {
			return err
		}
	}

	return nil
}

func (self *Agent) restoreState(event Event) error {
	calledTool, isKnown := self.owners[event.Name]
	if !isKnown {
		return nil
	}
	if err := calledTool.Restore(event.State); err != nil {
		return fmt.Errorf("could not restore %s state: %w", event.Name, err)
	}

	return nil
}

func (self *Agent) Tool(name string) (tool.Tool, bool) {
	found, isKnown := self.registeredTools[name]
	return found, isKnown
}

func (self *Agent) IsToolEnabled(name string) bool {
	_, isEnabled := self.enabledToolNames[name]
	return isEnabled
}

func (self *Agent) parseCall(call ToolCall) (tool.ToolCall, string) {
	calledTool, isRegistered := self.registeredTools[call.Name]
	if !isRegistered || !self.IsToolEnabled(call.Name) {
		return nil, fmt.Sprintf("there is no tool called %q", call.Name)
	}

	parsedToolCall, err := calledTool.Parse(call.Arguments)
	if err != nil {
		return nil, err.Error()
	}

	return parsedToolCall, ""
}

func exec(ctx context.Context, call tool.ToolCall) (tool.ToolCallResult, bool) {
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
