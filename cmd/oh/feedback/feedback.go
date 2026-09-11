package feedback

import (
	"fmt"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/painter"
	"crdx.org/io/cmd/oh/schedule"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/oh/width"
)

type Source int

const (
	System Source = iota
	Command
	Config
	Confirmation
	Approval
)

func (self Source) IsDismissedByTyping() bool {
	switch self {
	case Command, Confirmation:
		return true
	case System, Config, Approval:
		return false
	default:
		return false
	}
}

type Message struct {
	Text         string
	Status       agent.Status
	HasOwnStyle  bool
	DismissAfter time.Duration
}

type State struct {
	source    Source
	message   Message
	expiresAt time.Time
}

func (self *State) Show(source Source, message Message, now time.Time) {
	*self = State{source: source, message: message}
	if message.DismissAfter > 0 {
		self.expiresAt = now.Add(message.DismissAfter)
	}
}

func (self *State) Clear(source Source) {
	if self.message.Text != "" && self.source == source {
		*self = State{}
	}
}

func (self *State) ClearOnTyping() {
	if self.source.IsDismissedByTyping() {
		*self = State{}
	}
}

func (self *State) ClearExpired(at time.Time) {
	if !self.expiresAt.IsZero() && !at.Before(self.expiresAt) {
		*self = State{}
	}
}

func (self *State) NextRefresh(at time.Time) time.Time {
	if self.expiresAt.IsZero() {
		return time.Time{}
	}

	return schedule.Soonest(schedule.NextTick(at, time.Second), self.expiresAt)
}

func (self *State) Message() Message {
	return self.message
}

func (self *State) IsEmpty() bool {
	return self.message.Text == ""
}

func (self *State) Render(columns int, now time.Time) []string {
	text := self.message.Text
	if countdown := self.countdown(now); countdown != "" {
		text += " " + countdown
	}

	if !self.message.HasOwnStyle {
		text = painter.NoticeStyle(self.message.Status).Over(text)
	}
	return width.Wrap(text, columns)
}

func (self *State) countdown(now time.Time) string {
	if self.expiresAt.IsZero() {
		return ""
	}

	remainingTime := self.expiresAt.Sub(now)
	if remainingTime <= 0 {
		return ""
	}

	secondsLeft := int((remainingTime + time.Second - 1) / time.Second)
	return style.Subtle(fmt.Sprintf("(dismissing in %ds)", secondsLeft))
}
