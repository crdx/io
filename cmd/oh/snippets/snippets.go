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
	defaultFunctionName = "default"
	argumentUsage       = "<args>"
)

type templateData struct {
	Arg  string
	Args []string
}

func New(configured map[string]string) (slash.CommandSet, error) {
	commands := make([]slash.Command, 0, len(configured))
	for _, name := range slices.Sorted(maps.Keys(configured)) {
		prompt := strings.TrimSpace(configured[name])
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

		allowsNoArguments := templateUsesFunction(promptTemplate, defaultFunctionName)
		command := slash.Command{
			Name: name,
			Run: func(context slash.Context, arguments []string) error {
				if len(arguments) == 0 && !allowsNoArguments {
					return slash.Usage()
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
		if !allowsNoArguments {
			command = command.WithRequiredArguments(argumentUsage)
		}
		commands = append(commands, command)
	}

	return slash.NewCommandSet("//", commands...)
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

func templateUsesFunction(promptTemplate *template.Template, name string) bool {
	return slices.ContainsFunc(promptTemplate.Templates(), func(associated *template.Template) bool {
		return associated.Tree != nil && nodeUsesFunction(associated.Root, name)
	})
}

func nodeUsesFunction(node parse.Node, name string) bool {
	if node == nil || reflect.ValueOf(node).IsNil() {
		return false
	}

	switch typed := node.(type) {
	case *parse.IdentifierNode:
		return typed.Ident == name
	case *parse.ListNode:
		return slices.ContainsFunc(typed.Nodes, func(child parse.Node) bool {
			return nodeUsesFunction(child, name)
		})
	case *parse.ActionNode:
		return nodeUsesFunction(typed.Pipe, name)
	case *parse.PipeNode:
		return slices.ContainsFunc(typed.Cmds, func(command *parse.CommandNode) bool {
			return nodeUsesFunction(command, name)
		})
	case *parse.CommandNode:
		return slices.ContainsFunc(typed.Args, func(argument parse.Node) bool {
			return nodeUsesFunction(argument, name)
		})
	case *parse.ChainNode:
		return nodeUsesFunction(typed.Node, name)
	case *parse.IfNode:
		return branchUsesFunction(&typed.BranchNode, name)
	case *parse.RangeNode:
		return branchUsesFunction(&typed.BranchNode, name)
	case *parse.WithNode:
		return branchUsesFunction(&typed.BranchNode, name)
	case *parse.TemplateNode:
		return nodeUsesFunction(typed.Pipe, name)
	default:
		return false
	}
}

func branchUsesFunction(branch *parse.BranchNode, name string) bool {
	return nodeUsesFunction(branch.Pipe, name) ||
		nodeUsesFunction(branch.List, name) ||
		nodeUsesFunction(branch.ElseList, name)
}
