package expose

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"crdx.org/io/tool"
)

const (
	actionAdd    = "add"
	actionRemove = "remove"
	actionList   = "list"
)

var actions = []string{actionAdd, actionRemove, actionList}

type Publication struct {
	Port uint16
	URL  string
}

type Ports interface {
	Expose(port uint16) (string, error)
	Hide(port uint16) error
	List() []Publication
}

type Args struct {
	Action string `json:"action"`
	Port   int    `json:"port,omitempty"`
}

func New(ports Ports) tool.Tool {
	return tool.Implement(
		tool.Definition{
			Name: "expose",
			Description: "make a port of this sandbox reachable from the user's machine, " +
				"so a server started with the job tool can be opened in their browser",
			Schema: tool.Schema{
				tool.Enum("action", "what to do", actions...),
				tool.Integer("port", "the TCP port inside the sandbox (for add and remove)").Optional(),
			},
		},
		Describe,
	).
		Validate(validate).
		Exec(func(_ context.Context, args Args) (string, tool.ToolCallMetrics, error) {
			return run(ports, args)
		})
}

func Describe(args Args) (string, string) {
	if args.Action == actionList {
		return "", args.Action
	}
	if args.Action == actionAdd {
		return strconv.Itoa(args.Port), ""
	}

	return strconv.Itoa(args.Port), args.Action
}

func validate(args Args) error {
	if !slices.Contains(actions, args.Action) {
		return fmt.Errorf("action is %q, and wants to be one of: %s", args.Action, strings.Join(actions, ", "))
	}
	if args.Action == actionList {
		if args.Port != 0 {
			return errors.New("port cannot be used for list")
		}

		return nil
	}
	if args.Port < 1 || args.Port > 65535 {
		return fmt.Errorf("port is %d, and wants to be a number from 1 to 65535", args.Port)
	}

	return nil
}

func run(ports Ports, args Args) (string, tool.ToolCallMetrics, error) {
	port, err := portOf(args)
	if err != nil {
		return "", tool.ToolCallMetrics{}, err
	}

	switch args.Action {
	case actionAdd:
		address, err := ports.Expose(port)
		if err != nil {
			return "", tool.ToolCallMetrics{}, err
		}

		return "Port " + strconv.Itoa(args.Port) + " is exposed at " + address +
			", which the user can open on their own machine.", tool.ToolCallMetrics{}, nil
	case actionRemove:
		if err := ports.Hide(port); err != nil {
			return "", tool.ToolCallMetrics{}, err
		}

		return "Port " + strconv.Itoa(args.Port) + " is no longer exposed.", tool.ToolCallMetrics{}, nil
	case actionList:
		return list(ports), tool.ToolCallMetrics{}, nil
	}

	return "", tool.ToolCallMetrics{}, fmt.Errorf("unknown action %q", args.Action)
}

func portOf(args Args) (uint16, error) {
	if args.Action == actionList {
		return 0, nil
	}
	if args.Port < 1 || args.Port > 65535 {
		return 0, fmt.Errorf("port is %d, and wants to be a number from 1 to 65535", args.Port)
	}

	return uint16(args.Port), nil
}

func list(ports Ports) string {
	publications := ports.List()
	if len(publications) == 0 {
		return "No ports are exposed."
	}

	lines := make([]string, 0, len(publications))
	for _, publication := range publications {
		lines = append(lines, strconv.Itoa(int(publication.Port))+"  "+publication.URL)
	}

	return strings.Join(lines, "\n")
}
