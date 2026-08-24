// Package metrics tracks conversation activity displayed by status segments.
package metrics

import (
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/internal/util"
)

// Tracker records turn count, context usage, and the latest token rate.
type Tracker struct {
	contextWindowTokens  int
	inputTokens          int
	turnsTaken           int
	streamedBytes        int
	streamStartedAt      time.Time
	activeStreamDuration time.Duration
	lastTurnRate         float64
}

// New constructs a tracker for a model context window.
func New(contextWindowTokens int) Tracker {
	return Tracker{contextWindowTokens: contextWindowTokens}
}

// BeginTurn clears stream accounting for a newly started turn.
func (self *Tracker) BeginTurn() {
	self.streamedBytes = 0
	self.streamStartedAt = time.Time{}
	self.activeStreamDuration = 0
}

// RecordDelta incorporates provisional model prose into the displayed metrics.
func (self *Tracker) RecordDelta(delta agent.Delta) {
	if self.streamStartedAt.IsZero() {
		self.streamStartedAt = time.Now()
	}
	self.streamedBytes += len(delta.Text)
}

// Record incorporates one conversation event into the displayed metrics.
func (self *Tracker) Record(event agent.Event) {
	if event.Kind == agent.UserMessageEvent {
		self.turnsTaken++
	}
	if event.Kind == agent.ModelMessageEvent || event.Kind == agent.ModelReasoningEvent {
		self.finishActiveStream()
	}
	if event.Usage != nil && event.Usage.InputTokens > 0 {
		self.inputTokens = event.Usage.InputTokens
	}
}

// Restore rebuilds durable metrics from stored conversation events.
func (self *Tracker) Restore(events []agent.Event) {
	self.inputTokens = 0
	self.turnsTaken = 0
	self.streamedBytes = 0

	for _, event := range events {
		if event.Kind == agent.UserMessageEvent {
			self.turnsTaken++
		}
		if event.Usage != nil && event.Usage.InputTokens > 0 {
			self.inputTokens = event.Usage.InputTokens
		}
	}
}

// FinishTurn records the token rate of a turn that produced model text.
func (self *Tracker) FinishTurn() {
	self.finishActiveStream()
	elapsed := self.activeStreamDuration.Seconds()
	if self.streamedBytes == 0 || elapsed <= 0 {
		return
	}

	self.lastTurnRate = float64(util.EstimateTokenCount(self.streamedBytes)) / elapsed
}

// TurnCount returns the number of user turns in the conversation.
func (self *Tracker) TurnCount() int {
	return self.turnsTaken
}

// LastTurnTokenRate returns the latest measured token rate when one is known.
func (self *Tracker) LastTurnTokenRate() (float64, bool) {
	return self.lastTurnRate, self.lastTurnRate > 0
}

// ContextUsage returns the latest reported input tokens and the model context window.
func (self *Tracker) ContextUsage() (int, int) {
	return self.inputTokens, self.contextWindowTokens
}

func (self *Tracker) finishActiveStream() {
	if self.streamStartedAt.IsZero() {
		return
	}

	self.activeStreamDuration += time.Since(self.streamStartedAt)
	self.streamStartedAt = time.Time{}
}
