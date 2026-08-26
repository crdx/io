package painter

import (
	"slices"
	"strings"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/caps"
)

type PendingMessages struct {
	messages []string
}

func NewPendingMessages(messages []string) *PendingMessages {
	return &PendingMessages{messages: slices.Clone(messages)}
}

func (self *PendingMessages) Replace(messages []string) {
	self.messages = slices.Clone(messages)
}

func (self *PendingMessages) Rows(columns int) []string {
	var rows []string

	for i, message := range self.messages {
		if i > 0 {
			rows = append(rows, "")
		}
		rows = append(rows, strings.Split(RenderSubmittedMessage(message, columns), "\n")...)
	}

	return rows
}

func renderModeMessage(event agent.Event) (string, bool) {
	return caps.ModeNotice(event)
}
