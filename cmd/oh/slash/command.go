package slash

import (
	"fmt"
	"strings"

	"crdx.org/io/agent"
)

type Context interface {
	Emit(agent.Event)
	Send(string)
	Notice(string)
}

type Command struct {
	Name string
	Run  func(Context, []string) error
}

type Invocation struct {
	Command   *Command
	Arguments []string
}

type CommandSet map[string]*Command

func New(commands ...Command) CommandSet {
	set := CommandSet{}

	for i := range commands {
		command := &commands[i]
		name := strings.TrimPrefix(command.Name, "/")
		if name == "" || !strings.HasPrefix(command.Name, "/") || strings.ContainsAny(name, " \t\r\n/") {
			panic(fmt.Sprintf("invalid slash command name %q", command.Name))
		}
		if command.Run == nil {
			panic(fmt.Sprintf("slash command %q has no handler", command.Name))
		}
		if _, exists := set[command.Name]; exists {
			panic(fmt.Sprintf("slash command %q is already registered", command.Name))
		}
		set[command.Name] = command
	}

	return set
}

func (self CommandSet) Find(message string) (Invocation, bool) {
	fields := strings.Fields(message)
	if len(fields) == 0 {
		return Invocation{}, false
	}

	command, found := self[fields[0]]
	if !found {
		return Invocation{}, false
	}

	return Invocation{Command: command, Arguments: fields[1:]}, true
}

func (self CommandSet) Complete(prefix string) (string, bool) {
	if !strings.HasPrefix(prefix, "/") || strings.ContainsAny(prefix, " \t\r\n") {
		return "", false
	}

	match := ""
	for name := range self {
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		if match != "" {
			return "", false
		}
		match = name
	}

	return match, match != ""
}
