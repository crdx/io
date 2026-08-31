package commands

import (
	"fmt"
	"slices"
	"strings"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/pathgrant"
	"crdx.org/io/cmd/oh/slash"
)

type PathGrants struct {
	Grant      func(path string, access pathgrant.Access) (agent.Event, error)
	Revoke     func(path string) (agent.Event, error)
	GetCurrent func() []pathgrant.Grant
}

func (self PathGrants) isConfigured() bool {
	return self.Grant != nil && self.Revoke != nil && self.GetCurrent != nil
}

func pathGrantCommands(grants PathGrants) []slash.Command {
	return []slash.Command{
		grantCommand(grants),
		grantsCommand(grants),
		revokeCommand(grants),
	}
}

func grantCommand(grants PathGrants) slash.Command {
	return slash.Command{
		Name:        "grant",
		Description: "Grant temporary read-only or read-write path access.",
		Run: func(context slash.Context, arguments slash.Arguments) error {
			accessText, path, found := strings.Cut(arguments.Text, " ")
			path = strings.TrimSpace(path)
			if !found || path == "" {
				return slash.Usage()
			}

			access := pathgrant.Access(strings.TrimSpace(accessText))
			if access != pathgrant.ReadAccess && access != pathgrant.WriteAccess {
				return slash.Usage()
			}
			event, err := grants.Grant(path, access)
			if err != nil {
				return err
			}
			context.Emit(event)
			return nil
		},
	}.
		WithArguments(string(pathgrant.ReadAccess), string(pathgrant.WriteAccess)).
		WithArgumentUsage("{read|write} <path>")
}

func grantsCommand(grants PathGrants) slash.Command {
	return slash.Command{
		Name:        "grants",
		Description: "List temporary pathname grants.",
		Run: func(context slash.Context, arguments slash.Arguments) error {
			if arguments.Text != "" {
				return slash.Usage()
			}
			context.Notice(formatPathGrants(grants.GetCurrent()))
			return nil
		},
	}
}

func revokeCommand(grants PathGrants) slash.Command {
	return slash.Command{
		Name:        "revoke",
		Description: "Revoke temporary pathname access.",
		Run: func(context slash.Context, arguments slash.Arguments) error {
			if arguments.Text == "" {
				return slash.Usage()
			}
			event, err := grants.Revoke(arguments.Text)
			if err != nil {
				return err
			}
			context.Emit(event)
			return nil
		},
	}.
		WithListedArguments(func() []string { return pathGrantPaths(grants.GetCurrent()) }).
		WithArgumentUsage("<path>")
}

func pathGrantPaths(grants []pathgrant.Grant) []string {
	paths := make([]string, 0, len(grants))
	for _, grant := range grants {
		paths = append(paths, grant.Path)
	}
	return paths
}

func formatPathGrants(grants []pathgrant.Grant) string {
	if len(grants) == 0 {
		return "No temporary path grants."
	}

	grantList := slices.Clone(grants)
	slices.SortFunc(grantList, func(left pathgrant.Grant, right pathgrant.Grant) int {
		return strings.Compare(left.Path, right.Path)
	})

	lines := make([]string, 0, len(grantList))
	for _, grant := range grantList {
		lines = append(lines, fmt.Sprintf("  %-5s  %s", grant.Access, grant.Path))
	}
	return "Temporary path grants:\n" + strings.Join(lines, "\n")
}
