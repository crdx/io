package painter

import (
	"slices"
	"strings"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/caps"
)

const unsentMark = "⏳"

type PendingMessages struct {
	messages []string
	isSent   bool
}

func NewPendingMessages(messages []string) *PendingMessages {
	return &PendingMessages{messages: slices.Clone(messages)}
}

func (self *PendingMessages) Replace(messages []string) {
	self.messages = slices.Clone(messages)
}

// MarkSent says that a turn has taken the messages, which are drawn as any other from then on.
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
	if self.isSent {
		return RenderSubmittedMessage(message, columns)
	}

	return RenderSubmittedMessage(unsentMark+" "+message, columns)
}

func renderModeMessage(event agent.Event) (string, bool) {
	return caps.ModeNotice(event)
}
