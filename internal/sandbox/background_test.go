package sandbox

import (
	"os"
	"os/exec"
	"slices"
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
