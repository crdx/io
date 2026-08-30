package dispatch

import (
	"fmt"
	"path/filepath"
	"strings"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/slash"
	"crdx.org/io/internal/util/pathutil"
)

type Result int

const (
	commandNotFoundMessage = "Command not found"
	minimumPathParts       = 2
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
	EmitEvent    func(agent.Event)
	SendPrompt   func(string)
	ShowFeedback func(string, agent.Status)
}

func Handle(registry slash.Registry, actions Actions, message string) (Result, string) {
	if isExistingPathMessage(message) {
		return Ordinary, ""
	}

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

func isExistingPathMessage(message string) bool {
	pathParts := 0
	for pathPart := range strings.SplitSeq(filepath.Clean(message), string(filepath.Separator)) {
		if pathPart == "" {
			continue
		}

		pathParts++
		if pathParts >= minimumPathParts {
			return pathutil.Exists(message)
		}
	}

	return false
}

func (self Actions) Emit(event agent.Event) {
	self.EmitEvent(event)
}

func (self Actions) Send(message string) {
	self.SendPrompt(message)
}

func (self Actions) Notice(message string) {
	self.ShowFeedback(message, agent.InfoStatus)
}

func (self Actions) Success(message string) {
	self.ShowFeedback(message, agent.SuccessStatus)
}
