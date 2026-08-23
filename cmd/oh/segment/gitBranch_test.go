package segment_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/segment/gitBranch"
	"crdx.org/io/cmd/oh/style"
)

func repositoryWithHead(t *testing.T, head string) string {
	t.Helper()

	workspaceDir := t.TempDir()
	gitDir := filepath.Join(workspaceDir, ".git")

	if err := os.MkdirAll(gitDir, 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte(head), 0o600); err != nil {
		t.Fatal(err)
	}

	return workspaceDir
}

func branchDrawnIn(t *testing.T, workspaceDir string) string {
	t.Helper()

	built, err := gitBranch.New(workspaceDir)(tomlOptions(""))
	if err != nil {
		t.Fatal(err)
	}

	return style.Plain(built.Render(segment.Context{}))
}

func TestTheBranchSegmentNamesWhatHeadPointsAt(t *testing.T) {
	workspaceDir := repositoryWithHead(t, "ref: refs/heads/feature/bars\n")

	if got := branchDrawnIn(t, workspaceDir); got != "feature/bars" {
		t.Errorf("expected the whole branch name below refs/heads, got %q", got)
	}
}

func TestTheBranchSegmentShortensADetachedHead(t *testing.T) {
	workspaceDir := repositoryWithHead(t, "1fd19004e0f4a2c8b4c5d6e7f8a9b0c1d2e3f4a5\n")

	if got := branchDrawnIn(t, workspaceDir); got != "1fd1900" {
		t.Errorf("expected a short hash for a detached head, got %q", got)
	}
}

func TestTheBranchSegmentFollowsAWorktreePointer(t *testing.T) {
	elsewhere := repositoryWithHead(t, "ref: refs/heads/elsewhere\n")

	workspaceDir := t.TempDir()
	pointer := "gitdir: " + filepath.Join(elsewhere, ".git") + "\n"

	if err := os.WriteFile(filepath.Join(workspaceDir, ".git"), []byte(pointer), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := branchDrawnIn(t, workspaceDir); got != "elsewhere" {
		t.Errorf("expected the branch of the repository pointed at, got %q", got)
	}
}

func TestTheBranchSegmentSaysNothingOutsideARepository(t *testing.T) {
	if got := branchDrawnIn(t, t.TempDir()); got != "" {
		t.Errorf("expected nothing where there is no repository, got %q", got)
	}
}

func TestTheBranchSegmentLooksAgainBetweenTurns(t *testing.T) {
	built, err := gitBranch.New(t.TempDir())(tomlOptions("rate = \"2s\"\n"))
	if err != nil {
		t.Fatal(err)
	}

	layout := segment.Layout{segment.BottomLeft: {built}}

	if got := layout.IdleRefreshInterval(); got != 2*time.Second {
		t.Errorf("expected the given rate to pace the idle redraw, got %s", got)
	}
}

func TestTheBranchSegmentRefusesARateThatRunsBackwards(t *testing.T) {
	if _, err := gitBranch.New(t.TempDir())(tomlOptions("rate = \"-1s\"\n")); err == nil {
		t.Fatal("expected a negative rate to be refused")
	}
}

func TestTheBranchSegmentOnlyReadsHeadOnceWithinItsRate(t *testing.T) {
	workspaceDir := repositoryWithHead(t, "ref: refs/heads/first\n")

	built, err := gitBranch.New(workspaceDir)(tomlOptions("rate = \"1h\"\n"))
	if err != nil {
		t.Fatal(err)
	}

	if got := style.Plain(built.Render(segment.Context{})); got != "first" {
		t.Fatalf("expected the branch as it stood, got %q", got)
	}

	head := filepath.Join(workspaceDir, ".git", "HEAD")
	if err := os.WriteFile(head, []byte("ref: refs/heads/second\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := style.Plain(built.Render(segment.Context{})); got != "first" {
		t.Errorf("expected the cached branch until the rate is up, got %q", got)
	}
}
