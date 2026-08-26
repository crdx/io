package turn

import (
	"context"
	"time"

	"crdx.org/io/agent"
)

type Event struct {
	Update agent.Update
	Err    error
}

type State struct {
	Running    bool
	Cancelled  bool
	Err        error
	Reason     error
	StartedAt  time.Time
	FinishedAt time.Time
}

type Stream struct {
	events chan Event
	cancel context.CancelCauseFunc
	state  State
}

func Start(assistant *agent.Agent, message string) *Stream {
	streamContext, cancel := context.WithCancelCause(context.Background())
	stream := Adopt(make(chan Event), cancel, State{Running: true, StartedAt: time.Now()})

	go func() {
		defer close(stream.events)
		defer cancel(nil)
		for update, err := range assistant.Stream(streamContext, message) {
			stream.events <- Event{Update: update, Err: err}
			if err != nil {
				return
			}
		}
	}()
	return stream
}

func Adopt(events chan Event, cancel context.CancelCauseFunc, state State) *Stream {
	return &Stream{events: events, cancel: cancel, state: state}
}

func (self *Stream) Events() <-chan Event {
	if self == nil {
		return nil
	}
	return self.events
}

func (self *Stream) Running() bool   { return self != nil && self.state.Running }
func (self *Stream) Cancelled() bool { return self != nil && self.state.Cancelled }

func (self *Stream) Error() error {
	if self == nil {
		return nil
	}
	return self.state.Err
}

func (self *Stream) Elapsed() (bool, time.Duration, bool) {
	if self == nil || self.state.StartedAt.IsZero() {
		return false, 0, false
	}
	if self.state.FinishedAt.IsZero() {
		return self.state.Running, time.Since(self.state.StartedAt), true
	}
	return false, time.Since(self.state.FinishedAt), true
}

func (self *Stream) Interrupt(reason error) bool {
	if !self.Running() {
		return false
	}
	self.state.Cancelled = true
	self.state.Reason = reason
	self.cancel(reason)
	return true
}

func (self *Stream) Reason() error {
	if self == nil {
		return nil
	}
	return self.state.Reason
}

func (self *Stream) Observe(event Event) bool {
	if event.Err != nil {
		self.state.Err = event.Err
		return false
	}
	return true
}

func (self *Stream) SetCancelled(cancelled bool) { self.state.Cancelled = cancelled }
func (self *Stream) MarkFinished(at time.Time)   { self.state.FinishedAt = at }

func (self *Stream) Finish() {
	self.state.Running = false
	self.events = nil
}
