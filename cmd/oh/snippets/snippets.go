package snippets

import (
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"text/template"
	"text/template/parse"

	"crdx.org/io/cmd/oh/slash"
)

const (
	snippetPrefix         = "//"
	helpCommandName       = "help"
	defaultFunctionName   = "default"
	requiredArgumentUsage = "<args>"
	optionalArgumentUsage = "[args]"
	argumentFieldName     = "Arg"
	argumentsFieldName    = "Args"
)

type templateData struct {
	Arg  string
	Args []string
}

func New(configured map[string]Definition) (slash.CommandSet, error) {
	commands := make([]slash.Command, 0, len(configured))
	for _, name := range slices.Sorted(maps.Keys(configured)) {
		definition := configured[name]
		prompt := strings.TrimSpace(definition.Prompt)
		if prompt == "" {
			return slash.CommandSet{}, fmt.Errorf("%s: prompt is empty", name)
		}
		promptTemplate, err := template.New(name).
			Funcs(template.FuncMap{defaultFunctionName: defaultValue}).
			Option("missingkey=error").
			Parse(prompt)
		if err != nil {
			return slash.CommandSet{}, fmt.Errorf("%s: %w", name, err)
		}

		argumentPolicy := definition.Arguments
		if argumentPolicy == "" {
			argumentPolicy = inferArgumentPolicyFromTemplate(promptTemplate.Tree)
		}
		if argumentPolicy != ArgumentsRequired && argumentPolicy != ArgumentsOptional && argumentPolicy != ArgumentsNone {
			return slash.CommandSet{}, fmt.Errorf("%s: invalid argument policy %q", name, argumentPolicy)
		}
		command := slash.Command{
			Name:        name,
			Description: definition.Description,
			Run: func(context slash.Context, arguments []string) error {
				switch argumentPolicy {
				case ArgumentsRequired:
					if len(arguments) == 0 {
						return slash.Usage()
					}
				case ArgumentsNone:
					if len(arguments) != 0 {
						return slash.Usage()
					}
				}

				var rendered strings.Builder
				data := templateData{
					Arg:  strings.Join(arguments, " "),
					Args: arguments,
				}
				if err := promptTemplate.Execute(&rendered, data); err != nil {
					return fmt.Errorf("could not render template: %w", err)
				}
				if strings.TrimSpace(rendered.String()) == "" {
					return errors.New("template rendered an empty prompt")
				}
				context.Send(rendered.String())
				return nil
			},
		}
		switch argumentPolicy {
		case ArgumentsRequired:
			command = command.WithArgumentUsage(requiredArgumentUsage)
		case ArgumentsOptional:
			command = command.WithArgumentUsage(optionalArgumentUsage)
		}
		commands = append(commands, command)
	}

	var set slash.CommandSet
	var help slash.Command
	help = slash.Command{
		Name: helpCommandName,
		Run: func(context slash.Context, arguments []string) error {
			if len(arguments) != 0 {
				return slash.Usage()
			}

			context.Notice(helpText(set.GetHelpEntries(), snippetPrefix+help.Name))
			return nil
		},
	}
	commands = append(commands, help)

	var err error
	set, err = slash.NewCommandSet(snippetPrefix, commands...)
	return set, err
}

func helpText(entries []slash.HelpEntry, hiddenUsage string) string {
	visible := slices.DeleteFunc(entries, func(entry slash.HelpEntry) bool { return entry.Usage == hiddenUsage })
	if len(visible) == 0 {
		return "No snippets are configured."
	}
	return "Snippets:\n" + strings.Join(slash.FormatHelp(visible), "\n")
}

func inferArgumentPolicyFromTemplate(tree *parse.Tree) ArgumentPolicy {
	referencesArguments := false
	usesDefault := false
	walkTemplate(tree.Root, func(node parse.Node) {
		switch typed := node.(type) {
		case *parse.FieldNode:
			referencesArguments = referencesArguments || isArgumentField(typed.Ident)
		case *parse.IdentifierNode:
			usesDefault = usesDefault || typed.Ident == defaultFunctionName
		}
	})

	switch {
	case !referencesArguments:
		return ArgumentsNone
	case usesDefault:
		return ArgumentsOptional
	default:
		return ArgumentsRequired
	}
}

func isArgumentField(identifiers []string) bool {
	return len(identifiers) > 0 &&
		(identifiers[0] == argumentFieldName || identifiers[0] == argumentsFieldName)
}

func walkTemplate(node parse.Node, visit func(parse.Node)) {
	if node == nil || reflect.ValueOf(node).IsNil() {
		return
	}
	visit(node)

	switch typed := node.(type) {
	case *parse.ListNode:
		for _, child := range typed.Nodes {
			walkTemplate(child, visit)
		}
	case *parse.ActionNode:
		walkTemplate(typed.Pipe, visit)
	case *parse.PipeNode:
		for _, command := range typed.Cmds {
			walkTemplate(command, visit)
		}
	case *parse.CommandNode:
		for _, argument := range typed.Args {
			walkTemplate(argument, visit)
		}
	case *parse.IfNode:
		walkBranch(typed.BranchNode, visit)
	case *parse.RangeNode:
		walkBranch(typed.BranchNode, visit)
	case *parse.WithNode:
		walkBranch(typed.BranchNode, visit)
	case *parse.TemplateNode:
		walkTemplate(typed.Pipe, visit)
	case *parse.ChainNode:
		walkTemplate(typed.Node, visit)
	}
}

func walkBranch(branch parse.BranchNode, visit func(parse.Node)) {
	walkTemplate(branch.Pipe, visit)
	walkTemplate(branch.List, visit)
	walkTemplate(branch.ElseList, visit)
}

func defaultValue(fallback, value any) any {
	if isEmpty(value) {
		return fallback
	}
	return value
}

func isEmpty(value any) bool {
	reflected := reflect.ValueOf(value)
	if !reflected.IsValid() {
		return true
	}

	switch reflected.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return reflected.Len() == 0
	case reflect.Bool:
		return !reflected.Bool()
	case reflect.Complex64, reflect.Complex128:
		return reflected.Complex() == 0
	case reflect.Chan, reflect.Func, reflect.Pointer, reflect.UnsafePointer, reflect.Interface:
		return reflected.IsNil()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return reflected.Int() == 0
	case reflect.Float32, reflect.Float64:
		return reflected.Float() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return reflected.Uint() == 0
	default:
		return false
	}
}
