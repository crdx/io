package job_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"crdx.org/io/internal/jobs"
	"crdx.org/io/internal/sandbox"
	"crdx.org/io/tool"
	"crdx.org/io/toolbox/job"
)

func run(t *testing.T, manager *jobs.Manager, arguments map[string]string) (string, error) {
	t.Helper()

	encoded, err := json.Marshal(arguments)
	if err != nil {
		t.Fatal(err)
	}

	built := job.New(manager, nil, func(context.Context) (sandbox.Policy, error) {
		return sandbox.Policy{}, nil
	})

	call, err := built.Parse(string(encoded))
	if err != nil {
		return "", err
	}

	result, err := call.Exec(t.Context())

	return result.Output, err
}

func withFinishedJobs(t *testing.T) *jobs.Manager {
	t.Helper()

	manager := jobs.New(nil)
	manager.Restore([]jobs.Snapshot{
		{Name: "build", Command: "just build", State: jobs.StateFailed, Code: 1},
		{Name: "watch", Command: "just watch", State: jobs.StateComplete},
	})

	return manager
}

func TestPruningNamesWhatItRemoved(t *testing.T) {
	output, err := run(t, withFinishedJobs(t), map[string]string{"action": "prune"})
	if err != nil {
		t.Fatal(err)
	}

	if output != "pruned build, watch." {
		t.Errorf("got %q, want it to name what it pruned", output)
	}
}

func TestPruningNothingSaysSo(t *testing.T) {
	output, err := run(t, jobs.New(nil), map[string]string{"action": "prune"})
	if err != nil {
		t.Fatal(err)
	}

	if output != "there are no finished jobs to prune." {
		t.Errorf("got %q, want it to say there was nothing to do", output)
	}
}

func TestDiscardingReportsTheJobItRemoved(t *testing.T) {
	output, err := run(t, withFinishedJobs(t), map[string]string{"action": "discard", "name": "build"})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(output, "build: failed") || !strings.HasSuffix(output, "and discarded") {
		t.Errorf("got %q, want the job described and marked discarded", output)
	}
}

func TestListingNamesEveryJobWithItsCommand(t *testing.T) {
	output, err := run(t, withFinishedJobs(t), map[string]string{"action": "list"})
	if err != nil {
		t.Fatal(err)
	}

	for _, wanted := range []string{"build: failed", "just build", "watch: complete", "just watch"} {
		if !strings.Contains(output, wanted) {
			t.Errorf("got %q, want it to carry %q", output, wanted)
		}
	}
}

func TestAnEmptyListingSaysSo(t *testing.T) {
	output, err := run(t, jobs.New(nil), map[string]string{"action": "list"})
	if err != nil {
		t.Fatal(err)
	}

	if output != "no jobs have been started in this session." {
		t.Errorf("got %q, want it to say nothing has been started", output)
	}
}

func TestStartingAnUnknownNameWithNoCommandIsRefused(t *testing.T) {
	_, err := run(t, jobs.New(nil), map[string]string{"action": "start", "name": "ghost"})
	if err == nil || !strings.Contains(err.Error(), "command is required") {
		t.Errorf("got %v, want it to ask for a command", err)
	}
}

func TestWaitingOnAFinishedJobReportsItAtOnce(t *testing.T) {
	output, err := run(t, withFinishedJobs(t), map[string]string{"action": "wait", "name": "build"})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(output, "build: failed") {
		t.Errorf("got %q, want the job reported without any waiting at all", output)
	}
}

func TestAnUnknownActionIsRefused(t *testing.T) {
	_, err := run(t, jobs.New(nil), map[string]string{"action": "frobnicate", "name": "docs"})
	if err == nil || !strings.Contains(err.Error(), "wants to be one of") {
		t.Errorf("got %v, want the actions listed", err)
	}
}

func TestEveryActionButListAndPruneNeedsAName(t *testing.T) {
	for _, action := range []string{"status", "output", "wait", "stop", "discard", "start"} {
		if _, err := run(t, jobs.New(nil), map[string]string{"action": action}); err == nil ||
			!strings.Contains(err.Error(), "name is required") {
			t.Errorf("%s gave %v, want it to ask for a name", action, err)
		}
	}
}

func TestAStartIsRenderedByItsCommandAndAnyOtherActionByItsName(t *testing.T) {
	subject, qualifier := job.Describe(job.Args{Action: "start", Name: "docs", Command: "python3  -m\nhttp.server"})
	if subject != "python3 -m http.server" || qualifier != "docs" {
		t.Errorf("got %q / %q, want the command collapsed onto one line", subject, qualifier)
	}

	subject, qualifier = job.Describe(job.Args{Action: "status", Name: "docs"})
	if subject != "docs" || qualifier != "status" {
		t.Errorf("got %q / %q, want the name and the action", subject, qualifier)
	}

	subject, qualifier = job.Describe(job.Args{Action: "start", Name: "docs"})
	if subject != "docs" || qualifier != "start" {
		t.Errorf("got %q / %q, want a restart to read by name", subject, qualifier)
	}
}

var _ tool.Tool = job.New(nil, nil, nil)
