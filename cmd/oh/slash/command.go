package slash

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"crdx.org/io/agent"
)

type usageError struct{}

func (usageError) Error() string {
	return "invalid command usage"
}

func Usage() error {
	return usageError{}
}

func IsUsageError(err error) bool {
	var target usageError
	return errors.As(err, &target)
}

func FormatError(invocation Invocation, err error) string {
	if IsUsageError(err) {
		return "Usage: " + invocation.Usage
	}

	message := []rune(err.Error())
	if len(message) > 0 {
		message[0] = []rune(strings.ToUpper(string(message[0])))[0]
	}
	return invocation.Name + ": " + string(message)
}

type Context interface {
	Emit(agent.Event)
	Send(string)
	Notice(string)
	Success(string)
}

type Command struct {
	Name          string
	Run           func(Context, []string) error
	arguments     []string
	argumentUsage string
}

func (self Command) WithArguments(arguments ...string) Command {
	self.arguments = append([]string(nil), arguments...)
	return self
}

func (self Command) WithRequiredArguments(usage string) Command {
	self.argumentUsage = usage
	return self
}

func (self Command) usage(prefix string) string {
	name := prefix + self.Name
	if self.argumentUsage != "" {
		return name + " " + self.argumentUsage
	}
	if len(self.arguments) == 0 {
		return name
	}

	arguments := append([]string(nil), self.arguments...)
	slices.Sort(arguments)
	return name + " {" + strings.Join(arguments, "|") + "}"
}

type Invocation struct {
	Name      string
	Usage     string
	Command   *Command
	Arguments []string
}

type CommandSet struct {
	prefix   string
	commands map[string]*Command
}

func NewCommandSet(prefix string, commands ...Command) (CommandSet, error) {
	if err := validatePrefix(prefix); err != nil {
		return CommandSet{}, err
	}

	set := CommandSet{prefix: prefix, commands: make(map[string]*Command, len(commands))}
	for i := range commands {
		command := &commands[i]
		if command.Name == "" || strings.ContainsRune(command.Name, '/') || strings.ContainsFunc(command.Name, unicode.IsSpace) {
			return CommandSet{}, fmt.Errorf("invalid command name %q", command.Name)
		}
		if command.Run == nil {
			return CommandSet{}, fmt.Errorf("command %q has no handler", prefix+command.Name)
		}
		if command.argumentUsage != "" && len(command.arguments) > 0 {
			return CommandSet{}, fmt.Errorf("command %q has conflicting argument metadata", prefix+command.Name)
		}
		if _, exists := set.commands[command.Name]; exists {
			return CommandSet{}, fmt.Errorf("command %q is already registered", prefix+command.Name)
		}
		set.commands[command.Name] = command
	}

	return set, nil
}

func (self CommandSet) Usages() []string {
	usages := make([]string, 0, len(self.commands))
	for _, command := range self.commands {
		usages = append(usages, command.usage(self.prefix))
	}
	slices.Sort(usages)
	return usages
}

type Registry struct {
	sets []CommandSet
}

func NewRegistry(sets ...CommandSet) (Registry, error) {
	prefixes := make(map[string]struct{}, len(sets))
	for _, set := range sets {
		if err := validatePrefix(set.prefix); err != nil {
			return Registry{}, err
		}
		if _, exists := prefixes[set.prefix]; exists {
			return Registry{}, fmt.Errorf("command prefix %q is already registered", set.prefix)
		}
		prefixes[set.prefix] = struct{}{}
	}

	registered := append([]CommandSet(nil), sets...)
	slices.SortStableFunc(registered, func(left, right CommandSet) int {
		return len(right.prefix) - len(left.prefix)
	})
	return Registry{sets: registered}, nil
}

func validatePrefix(prefix string) error {
	if prefix == "" || strings.ContainsFunc(prefix, unicode.IsSpace) {
		return fmt.Errorf("invalid command prefix %q", prefix)
	}
	return nil
}

func (self Registry) Find(message string) (Invocation, bool) {
	fields := strings.Fields(message)
	if len(fields) == 0 {
		return Invocation{}, false
	}

	set, found := self.getSet(fields[0])
	if !found {
		return Invocation{}, false
	}

	bareName := strings.TrimPrefix(fields[0], set.prefix)
	command, found := set.commands[bareName]
	if !found {
		return Invocation{}, false
	}

	return Invocation{
		Name:      set.prefix + command.Name,
		Usage:     command.usage(set.prefix),
		Command:   command,
		Arguments: fields[1:],
	}, true
}

func (self Registry) CommandName(message string) (string, bool) {
	fields := strings.Fields(message)
	if len(fields) == 0 {
		return "", false
	}
	if _, found := self.getSet(fields[0]); !found {
		return "", false
	}
	return fields[0], true
}

func (self Registry) getSet(name string) (CommandSet, bool) {
	for _, set := range self.sets {
		if strings.HasPrefix(name, set.prefix) {
			return set, true
		}
	}
	return CommandSet{}, false
}

type Completion struct {
	matches []string
	current string
	index   int
}

func (self *Completion) Next(registry Registry, prefix string) (string, bool) {
	if prefix == self.current && len(self.matches) > 0 {
		self.index = (self.index + 1) % len(self.matches)
		self.current = self.matches[self.index]
		return self.current, true
	}

	self.matches = registry.completions(prefix)
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

func (self Registry) completions(prefix string) []string {
	if strings.ContainsAny(prefix, "\t\r\n") {
		return nil
	}

	name, argumentPrefix, hasArgument := strings.Cut(prefix, " ")
	set, found := self.getSet(name)
	if !found {
		return nil
	}

	bareName := strings.TrimPrefix(name, set.prefix)
	if !hasArgument {
		matches := matchingPrefixes(bareName, set.commandNames())
		for i := range matches {
			matches[i] = set.prefix + matches[i]
		}
		return matches
	}
	if strings.Contains(argumentPrefix, " ") {
		return nil
	}

	command, found := set.commands[bareName]
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
	names := make([]string, 0, len(self.commands))
	for name := range self.commands {
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
