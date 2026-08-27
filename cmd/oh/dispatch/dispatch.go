package dispatch

import (
	"fmt"
	"strings"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/slash"
)

type Result int

const (
	commandNotFoundMessage = "Command not found"
	sendAsMessageHint      = " (alt+enter sends as message)"
	snippetNotFoundMessage = "Snippet not found"
	snippetPrefix          = "//"
)

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
			return Rejected, slash.FormatError(invocation, err) + sendAsMessageHint
		}
		return Handled, ""
	}

	name, isCommand := registry.CommandName(message)
	if !isCommand {
		return Ordinary, ""
	}

	notFoundMessage := commandNotFoundMessage
	if strings.HasPrefix(name, snippetPrefix) {
		notFoundMessage = snippetNotFoundMessage
	}
	return Rejected, fmt.Sprintf("%s: %s%s", notFoundMessage, name, sendAsMessageHint)
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
