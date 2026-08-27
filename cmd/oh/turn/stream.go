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
	Timing     Timing
}

type Stream struct {
	events chan Event
	cancel context.CancelCauseFunc
	state  State
}

type Timing struct {
	UserTurn  time.Duration
	ModelTurn time.Duration
}

func Start(assistant *agent.Agent, message string, timing Timing) *Stream {
	streamContext, cancel := context.WithCancelCause(context.Background())
	stream := Adopt(make(chan Event), cancel, State{Running: true, StartedAt: time.Now(), Timing: timing})

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

func (self *Stream) Timing() (Timing, bool) {
	if self == nil || self.state.StartedAt.IsZero() {
		return Timing{}, false
	}
	timing := self.state.Timing
	if self.state.FinishedAt.IsZero() {
		timing.ModelTurn = time.Since(self.state.StartedAt)
		return timing, true
	}
	timing.UserTurn = time.Since(self.state.FinishedAt)
	timing.ModelTurn = self.state.FinishedAt.Sub(self.state.StartedAt)
	return timing, true
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
