package painter

import (
	"slices"
	"strings"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/pathgrant"
)

const unsentMark = "⏳"

type PendingMessages struct {
	messages               []string
	isSent                 bool
	shouldRenderHyperlinks bool
}

func NewPendingMessages(messages []string, shouldRenderHyperlinks bool) *PendingMessages {
	return &PendingMessages{
		messages:               slices.Clone(messages),
		shouldRenderHyperlinks: shouldRenderHyperlinks,
	}
}

func (self *PendingMessages) Replace(messages []string) {
	self.messages = slices.Clone(messages)
}

func (self *PendingMessages) MarkSent() {
	self.isSent = true
}

func (self *PendingMessages) Rows(columns int) []string {
	var rows []string

	for i, message := range self.messages {
		if i > 0 {
			rows = append(rows, "")
		}
		rows = append(rows, strings.Split(self.render(message, columns), "\n")...)
	}

	return rows
}

func (self *PendingMessages) render(message string, columns int) string {
	if !self.isSent {
		message = unsentMark + " " + message
	}
	if self.shouldRenderHyperlinks {
		return RenderSubmittedMessageWithHyperlinks(message, columns)
	}

	return RenderSubmittedMessage(message, columns)
}

func renderAccessMessage(event agent.Event) (string, bool) {
	switch event.Kind {
	case caps.ModeChange:
		return caps.ModeNotice(event)
	case pathgrant.Change:
		return pathgrant.Notice(event)
	case agent.StartupEvent, agent.UserMessageEvent, agent.HarnessMessageEvent,
		agent.ModelReasoningEvent, agent.ModelMessageEvent, agent.ToolCallRequestEvent,
		agent.ToolCallResultEvent, agent.StateChangeEvent, agent.InterruptionEvent,
		agent.RetryingEvent, agent.FailureEvent:
		return "", false
	}
	return "", false
}
