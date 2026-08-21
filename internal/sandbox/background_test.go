package sandbox

import (
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

func TestProcessTreesAreDrawnAsBranches(t *testing.T) {
	if got := formatProcessTree("a", []string{"b → c", "d"}); got != "a → b → c, d" {
		t.Errorf("got %q", got)
	}
}

func TestProcessNamesAreSafeToDraw(t *testing.T) {
	if got := safeProcessName("tmux: server\n"); got != "tmux: server" {
		t.Errorf("got %q", got)
	}
	if got := safeProcessName("bad,(name)\x1b\n"); got != "bad??name??" {
		t.Errorf("got %q", got)
	}
}

func TestProcessTreesNameDescendantsAndNotTheirSupervisor(t *testing.T) {
	child := exec.Command("sleep", "30")
	if err := child.Start(); err != nil {
		t.Skipf("sleep is unavailable: %v", err)
	}
	defer func() {
		_ = child.Process.Kill()
		_ = child.Wait()
	}()

	trees := processTrees(os.Getpid())
	if len(trees) == 0 {
		t.Skip("proc descendants are unavailable")
	}
	if !slices.Contains(trees, "sleep") {
		t.Errorf("expected the child name in %v", trees)
	}
	if slices.Contains(trees, "sandbox.test") {
		t.Errorf("the supervisor named itself in %v", trees)
	}
}

func TestNothingIsNamedForAProcessTheMachineWillNotDescribe(t *testing.T) {
	const noSuchProcess = -1

	if children := childProcesses(noSuchProcess); children != nil {
		t.Errorf("got %v, want no children of a process that is not there", children)
	}
	if name := processName(noSuchProcess); name != "" {
		t.Errorf("got %q, want no name for a process that is not there", name)
	}
	if trees := processTrees(noSuchProcess); trees != nil {
		t.Errorf("got %v, want no tree for a process that is not there", trees)
	}
}

func TestAProcessTreeIsDrawnWithoutItsBranchesWhereThereAreNone(t *testing.T) {
	if got := formatProcessTree("alone", nil); got != "alone" {
		t.Errorf("got %q, want the name on its own", got)
	}
	if got := formatProcessTree("", []string{"b", "c"}); got != "b, c" {
		t.Errorf("got %q, want the branches of a process with no name", got)
	}
}

func TestASetThatIsNotRunningAnythingStopsCleanly(t *testing.T) {
	processes := NewProcesses(true)

	names, err := processes.Disable()
	if err != nil {
		t.Errorf("got %v, want a clean stop", err)
	}
	if names != nil {
		t.Errorf("got %v, want nothing named", names)
	}
	if processes.backgroundEnabled {
		t.Error("the set is still handing out background processes")
	}

	processes.Enable()
	if !processes.backgroundEnabled {
		t.Error("the set did not start handing out background processes again")
	}
}

func TestADisabledSetRunsItsCommandInTheForeground(t *testing.T) {
	policy := Policy{Background: true, FileSize: -1} // refused before a namespace is asked for

	_, err := NewProcesses(false).Run(t.Context(), t.TempDir(), "true", policy)

	if err == nil || !strings.Contains(err.Error(), "is not a size") {
		t.Errorf("got %v, want the foreground refusal of the policy", err)
	}
	if err != nil && strings.Contains(err.Error(), "needs a process set") {
		t.Errorf("got %v, want the background flag cleared by the fallback", err)
	}
}

func TestAnEnabledSetRefusesAPolicyItCannotEnforce(t *testing.T) {
	processes := NewProcesses(true)
	defer func() { _, _ = processes.Disable() }()

	_, err := processes.Run(t.Context(), t.TempDir(), "true", Policy{OpenFiles: -1})

	if err == nil || !strings.Contains(err.Error(), "is not a count") {
		t.Errorf("got %v, want the policy refused before anything ran", err)
	}
}
