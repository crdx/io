package dispatch

import (
	"fmt"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/slash"
)

type Result int

const sendAnywayHint = "; press alt+enter to send anyway"

const (
	Ordinary Result = iota
	Handled
	Rejected
)

type Actions struct {
	EmitEvent  func(agent.Event)
	SendPrompt func(string)
}

func Handle(registry slash.Registry, actions Actions, message string) (Result, string) {
	invocation, found := registry.Find(message)
	if found {
		if err := invocation.Command.Run(actions, invocation.Arguments); err != nil {
			failure := slash.FormatError(invocation, err)
			if slash.IsUsageError(err) {
				return Rejected, failure + sendAnywayHint
			}
			return Handled, failure
		}
		return Handled, ""
	}

	name, isCommand := registry.CommandName(message)
	if !isCommand {
		return Ordinary, ""
	}

	return Rejected, fmt.Sprintf("Command not found: %s%s", name, sendAnywayHint)
}

func (self Actions) Emit(event agent.Event) {
	self.EmitEvent(event)
}

func (self Actions) Send(message string) {
	self.SendPrompt(message)
}

func (self Actions) Notice(message string) {
	self.Emit(agent.Event{Kind: agent.HarnessMessageEvent, Text: message, Status: agent.InfoStatus})
}

func (self Actions) Success(message string) {
	self.Emit(agent.Event{Kind: agent.HarnessMessageEvent, Text: message, Status: agent.SuccessStatus})
}
