package metrics

import "crdx.org/io/agent"

type Tracker struct {
	contextWindowTokens int
	inputTokens         int
	turnsTaken          int
}

func New(contextWindowTokens int) Tracker {
	return Tracker{contextWindowTokens: contextWindowTokens}
}

func (self *Tracker) BeginTurn() {
	self.turnsTaken++
}

func (self *Tracker) Record(event agent.Event) {
	if event.Usage != nil && event.Usage.InputTokens > 0 {
		self.inputTokens = event.Usage.InputTokens
	}
}

func (self *Tracker) Restore(events []agent.Event, completedTurns int) {
	self.inputTokens = 0
	self.turnsTaken = completedTurns

	for _, event := range events {
		if event.Usage != nil && event.Usage.InputTokens > 0 {
			self.inputTokens = event.Usage.InputTokens
		}
	}
}

func (self *Tracker) TurnCount() int {
	return self.turnsTaken
}

func (self *Tracker) ContextUsage() (int, int) {
	return self.inputTokens, self.contextWindowTokens
}
