package job

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"crdx.org/io/internal/file"
	"crdx.org/io/internal/jobs"
	"crdx.org/io/internal/sandbox"
	"crdx.org/io/internal/util"
	"crdx.org/io/tool"
)

const (
	actionStart   = "start"
	actionStatus  = "status"
	actionOutput  = "output"
	actionWait    = "wait"
	actionStop    = "stop"
	actionList    = "list"
	actionDiscard = "discard"
	actionPrune   = "prune"
)

const waitLimit = 5 * time.Minute

var actions = []string{
	actionStart,
	actionStatus,
	actionOutput,
	actionWait,
	actionStop,
	actionDiscard,
	actionPrune,
	actionList,
}

type Args struct {
	Action  string `json:"action"`
	Name    string `json:"name"`
	Command string `json:"command"`
}

func New(
	manager *jobs.Manager,
	root *file.Root,
	buildPolicy func(context.Context) (sandbox.Policy, error),
) tool.Tool {
	return tool.Implement(
		tool.Definition{
			Name:        "job",
			Description: "run a shell command in the background, returning immediately",
			Schema: tool.Schema{
				tool.Enum("action", "what to do", actions...),
				tool.String("name", "short name for the job (for all actions except 'list', 'prune'); e.g. check, lint, build").Optional(),
				tool.String("command", "the command line (for action 'start'); if omitted, re-runs previous job by name").Optional(),
			},
		},
		Describe,
	).
		Validate(validate).
		Exec(func(ctx context.Context, args Args) (string, tool.ToolCallMetrics, error) {
			return run(ctx, manager, root, buildPolicy, args)
		})
}

func Describe(args Args) (string, string) {
	if args.Action == actionStart && strings.TrimSpace(args.Command) != "" {
		return strings.Join(strings.Fields(args.Command), " "), args.Name
	}

	return args.Name, args.Action
}

func validate(args Args) error {
	if !slices.Contains(actions, args.Action) {
		return fmt.Errorf("action is %q, and wants to be one of: %s", args.Action, strings.Join(actions, ", "))
	}

	if args.Action == actionList || args.Action == actionPrune {
		return nil
	}

	if strings.TrimSpace(args.Name) == "" {
		return errors.New("name is required")
	}

	return nil
}

func run(
	ctx context.Context,
	manager *jobs.Manager,
	root *file.Root,
	buildPolicy func(context.Context) (sandbox.Policy, error),
	args Args,
) (string, tool.ToolCallMetrics, error) {
	report, err := act(ctx, manager, root, buildPolicy, args)

	return report, tool.GetMetrics(report), err
}

func act(
	ctx context.Context,
	manager *jobs.Manager,
	root *file.Root,
	buildPolicy func(context.Context) (sandbox.Policy, error),
	args Args,
) (string, error) {
	switch args.Action {
	case actionStart:
		command := strings.TrimSpace(args.Command)
		if command == "" {
			rememberedCommand, isRemembered := manager.RememberedCommand(args.Name)
			if !isRemembered {
				return "", errors.New("command is required, since no earlier job of that name is remembered")
			}
			command = rememberedCommand
		}

		policy, err := buildPolicy(ctx)
		if err != nil {
			return "", err
		}

		snapshot, err := manager.Start(ctx, args.Name, root.Name(), command, policy)
		if err != nil {
			return "", err
		}

		return snapshot.Describe(), nil

	case actionStatus:
		snapshot, err := manager.Status(args.Name)
		if err != nil {
			return "", err
		}

		return snapshot.Describe(), nil

	case actionOutput:
		output, snapshot, err := manager.Output(args.Name)
		if err != nil {
			return "", err
		}

		return withOutput(snapshot.Describe(), output, snapshot.DroppedBytes), nil

	case actionWait:
		return waited(ctx, manager, args.Name, waitLimit)

	case actionDiscard:
		discardedJob, err := manager.Discard(args.Name)
		if err != nil {
			return "", err
		}

		return discardedJob.Describe() + ", and discarded", nil

	case actionPrune:
		prunedNames := manager.PruneFinished()
		if len(prunedNames) == 0 {
			return "there are no finished jobs to prune.", nil
		}

		return "pruned " + strings.Join(prunedNames, ", ") + ".", nil

	case actionStop:
		snapshot, err := manager.Stop(args.Name)
		if err != nil {
			return "", err
		}

		return snapshot.Describe(), nil

	default:
		return listing(manager.List()), nil
	}
}

func waited(ctx context.Context, manager *jobs.Manager, name string, limit time.Duration) (string, error) {
	waitContext, stopWaiting := context.WithTimeout(ctx, limit)
	defer stopWaiting()

	err := manager.Wait(waitContext, name)
	if err != nil && (ctx.Err() != nil || !errors.Is(err, context.DeadlineExceeded)) {
		return "", err
	}

	output, snapshot, err := manager.Output(name)
	if err != nil {
		return "", err
	}

	status := snapshot.Describe()
	if snapshot.IsLive() {
		status += fmt.Sprintf(
			"\nnote: the wait gave up after %s, and the job is still running.",
			util.CompactDuration(limit),
		)
	}

	return withOutput(status, output, snapshot.DroppedBytes), nil
}

func withOutput(status string, output string, droppedBytes int) string {
	lines := []string{status}

	if droppedBytes > 0 {
		lines = append(lines, fmt.Sprintf(
			"note: the oldest %s of output was dropped to keep the spool bounded.",
			util.FormatBytes(int64(droppedBytes), 3),
		))
	}

	if strings.TrimSpace(output) == "" {
		return strings.Join(append(lines, "the job has printed nothing."), "\n")
	}

	return strings.Join(append(lines, strings.TrimRight(output, "\n")), "\n")
}

func listing(snapshots []jobs.Snapshot) string {
	if len(snapshots) == 0 {
		return "no jobs have been started in this session."
	}

	lines := make([]string, 0, len(snapshots))
	for _, snapshot := range snapshots {
		lines = append(lines, snapshot.Describe()+" — "+snapshot.Command)
	}

	return strings.Join(lines, "\n")
}
