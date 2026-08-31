package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	childVariable     = "IO_SANDBOX_CHILD"
	workspaceVariable = "IO_SANDBOX_WORKSPACE"
	homeVariable      = "IO_SANDBOX_HOME"
)

func TestGitMayCommitWithinAWorkspaceItMayNotOtherwiseChange(t *testing.T) {
	if os.Getenv(childVariable) != "" {
		commitUnderLandlock(t)
		return
	}

	version, err := landlockVersion()
	if err != nil {
		t.Skipf("landlock cannot be asked here: %v", err)
	}

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("there is no git here to commit with")
	}

	workspace := t.TempDir()
	home := t.TempDir()

	repository(t, workspace, home)

	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	//nolint:gosec // the binary is this one
	child := exec.CommandContext(t.Context(), self, "-test.run", t.Name())
	child.Dir = workspace
	child.Env = []string{
		childVariable + "=1",
		workspaceVariable + "=" + workspace,
		homeVariable + "=" + home,
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"TMPDIR=" + home,
		"GOCOVERDIR=" + home,
		"GIT_CONFIG_NOSYSTEM=1",
	}

	if out, err := child.CombinedOutput(); err != nil {
		t.Errorf("landlock abi %d: %v\n%s", version, err, out)
	}
}

func repository(t *testing.T, workspace string, home string) {
	t.Helper()

	config := strings.Join([]string{
		"[user]", "name = test", "email = test@example.com",
		"[init]", "defaultBranch = main",
		"[commit]", "gpgsign = false",
	}, "\n")

	for path, body := range map[string]string{
		filepath.Join(home, ".gitconfig"): config,
		filepath.Join(workspace, "one"):   "one\n",
	} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"commit", "-q", "-m", "first"}} {
		//nolint:gosec // the arguments are written above
		command := exec.CommandContext(t.Context(), "git", args...)
		command.Dir = workspace
		command.Env = []string{"HOME=" + home, "PATH=" + os.Getenv("PATH"), "GIT_CONFIG_NOSYSTEM=1"}

		if out, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	if err := os.WriteFile(filepath.Join(workspace, "two"), []byte("two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func commitUnderLandlock(t *testing.T) {
	t.Helper()

	workspace := os.Getenv(workspaceVariable)
	home := os.Getenv(homeVariable)

	if err := AvailableAtAll(); err != nil {
		t.Fatal(err)
	}

	policy := Policy{
		Read:  []string{workspace},
		Write: []string{filepath.Join(workspace, ".git"), home},
		Exec:  []string{"/usr/bin", "/usr/local/bin"},
	}

	if _, err := applyLandlock(policy); err != nil {
		t.Fatalf("could not enter the sandbox: %v", err)
	}

	refusedPath := filepath.Join(workspace, "three")

	//nolint:gosec // the path is a temporary directory this test made
	if err := os.WriteFile(refusedPath, []byte("no\n"), 0o600); err == nil {
		t.Error("expected a workspace held read-only to refuse a new file")
	}

	for _, args := range [][]string{
		{"add", "-A"},
		{"commit", "-q", "-m", "second"},
		{"log", "--oneline"},
		{"gc", "--quiet"},
	} {
		//nolint:gosec // the arguments are written above
		command := exec.CommandContext(t.Context(), "git", args...)
		command.Dir = workspace
		command.Env = os.Environ()

		if out, err := command.CombinedOutput(); err != nil {
			t.Errorf("git %v: %v\n%s", args, err, out)
		}
	}
}
