package slash

import (
	"fmt"
	"slices"
	"strings"

	"crdx.org/io/agent"
)

type Context interface {
	Emit(agent.Event)
	Send(string)
	Notice(string)
}

type Command struct {
	Name      string
	Run       func(Context, []string) error
	arguments []string
}

func (self Command) WithArguments(arguments ...string) Command {
	self.arguments = append([]string(nil), arguments...)
	return self
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

func CommandName(message string) (string, bool) {
	fields := strings.Fields(message)
	if len(fields) == 0 || !strings.HasPrefix(fields[0], "/") {
		return "", false
	}
	return fields[0], true
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

type Completion struct {
	matches []string
	current string
	index   int
}

func (self *Completion) Next(commands CommandSet, prefix string) (string, bool) {
	if prefix == self.current && len(self.matches) > 0 {
		self.index = (self.index + 1) % len(self.matches)
		self.current = self.matches[self.index]
		return self.current, true
	}

	self.matches = commands.completions(prefix)
	self.index = 0
	if len(self.matches) == 0 {
		self.current = ""
		return "", false
	}

	self.current = self.matches[0]
	return self.current, true
}

func (self *Completion) Reset() {
	self.matches = nil
	self.current = ""
	self.index = 0
}

func (self CommandSet) completions(prefix string) []string {
	if !strings.HasPrefix(prefix, "/") || strings.ContainsAny(prefix, "\t\r\n") {
		return nil
	}

	name, argumentPrefix, hasArgument := strings.Cut(prefix, " ")
	if !hasArgument {
		return matchingPrefixes(name, self.commandNames())
	}
	if strings.Contains(argumentPrefix, " ") {
		return nil
	}

	command, found := self[name]
	if !found {
		return nil
	}

	arguments := matchingPrefixes(argumentPrefix, command.arguments)
	for i := range arguments {
		arguments[i] = name + " " + arguments[i]
	}
	return arguments
}

func (self CommandSet) commandNames() []string {
	names := make([]string, 0, len(self))
	for name := range self {
		names = append(names, name)
	}
	return names
}

func matchingPrefixes(prefix string, candidates []string) []string {
	var matches []string
	for _, candidate := range candidates {
		if strings.HasPrefix(candidate, prefix) {
			matches = append(matches, candidate)
		}
	}
	slices.Sort(matches)
	return matches
}
