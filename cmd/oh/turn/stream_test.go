package turn

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/tool"
)

type streamProvider struct {
	send func(context.Context, agent.Yield) (agent.Reply, error)
}

func (self streamProvider) Configure(string, []tool.Definition)   {}
func (self streamProvider) AddUserMessage(string)                 {}
func (self streamProvider) AddToolResults([]agent.ToolCallResult) {}
func (self streamProvider) Send(ctx context.Context, yield agent.Yield) (agent.Reply, error) {
	return self.send(ctx, yield)
}

func TestStreamLifecycle(t *testing.T) {
	provider := streamProvider{send: func(_ context.Context, yield agent.Yield) (agent.Reply, error) {
		yield(agent.Output{Kind: agent.ModelMessageEvent, Text: "hello"})
		return agent.Reply{}, nil
	}}
	stream := Start(agent.New("", provider, nil), "begin")
	var events []Event
	for event := range stream.Events() {
		events = append(events, event)
	}
	if len(events) == 0 || !stream.Running() {
		t.Fatal("stream did not deliver while running")
	}
	finishedAt := time.Now()
	stream.MarkFinished(finishedAt)
	if running, _, known := stream.Elapsed(); running || !known {
		t.Error("finished duration was not retained")
	}
	stream.Finish()
	if stream.Running() || stream.Events() != nil {
		t.Error("stream remained active")
	}
}

func TestInterruptReachesProvider(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		provider := streamProvider{send: func(ctx context.Context, _ agent.Yield) (agent.Reply, error) {
			<-ctx.Done()
			return agent.Reply{}, ctx.Err()
		}}
		stream := Start(agent.New("", provider, nil), "begin")
		synctest.Wait()
		if !stream.Interrupt() || !stream.Cancelled() {
			t.Fatal("stream was not interrupted")
		}
		for event := range stream.Events() {
			stream.Observe(event)
		}
		if stream.Error() == nil {
			t.Error("terminal cancellation error was lost")
		}
	})
}

func TestObserveAndExternalState(t *testing.T) {
	terminalError := errors.New("failed")
	startedAt := time.Now().Add(-time.Second)
	stream := Adopt(nil, func() {}, State{Running: true, StartedAt: startedAt})
	if stream.Observe(Event{Err: terminalError}) || !errors.Is(stream.Error(), terminalError) {
		t.Error("error was not retained")
	}
	if !stream.Observe(Event{Update: agent.Update{}}) {
		t.Error("update was refused")
	}
	stream.SetCancelled(true)
	if !stream.Cancelled() {
		t.Error("external cancellation was lost")
	}
}

func TestAbsentStreamIsIdle(t *testing.T) {
	var stream *Stream
	if stream.Running() || stream.Cancelled() || stream.Error() != nil || stream.Events() != nil {
		t.Error("absent stream is active")
	}
	if _, _, known := stream.Elapsed(); known {
		t.Error("absent stream has timing")
	}
}
