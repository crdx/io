package segment_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/segment/activitySpinner"
	"crdx.org/io/cmd/oh/segment/gitBranch"
	"crdx.org/io/cmd/oh/segment/localTime"
	"crdx.org/io/cmd/oh/segment/sessionEmoji"
	"crdx.org/io/cmd/oh/segment/sessionName"
	"crdx.org/io/cmd/oh/segment/turnCount"
	"crdx.org/io/cmd/oh/segment/turnTimer"
	"crdx.org/io/cmd/oh/style"
	"github.com/BurntSushi/toml"
)

type saying struct {
	text string
}

func (self saying) Render(segment.Context) string {
	return self.text
}

func offering(text string) segment.Factory {
	return func(options segment.Options) (segment.Segment, error) {
		if err := options.Read(&struct{}{}); err != nil {
			return nil, err
		}

		return saying{text: text}, nil
	}
}

type noOptions struct{}

func (noOptions) Read(any) error {
	return nil
}

func TestEveryPositionIsListedExactlyOnce(t *testing.T) {
	if len(segment.Positions) != 6 {
		t.Errorf("expected six positions, got %d", len(segment.Positions))
	}

	seen := map[segment.Position]bool{}
	for _, position := range segment.Positions {
		if seen[position] {
			t.Errorf("expected %s once, got it twice", position)
		}
		seen[position] = true
	}
}

func TestASegmentOnOfferIsBuilt(t *testing.T) {
	set := segment.Registry{"model": offering("gpt")}

	built, err := set.Build("model", segment.BottomLeft, noOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if got := style.Plain(built.Render(segment.Context{})); got != "gpt" {
		t.Errorf("expected the built segment to draw itself, got %q", got)
	}
}

func TestASegmentThatIsNotOfferedSaysWhereAndWhatInstead(t *testing.T) {
	set := segment.Registry{"model": offering("gpt"), "scroll": offering("↑ 3")}

	_, err := set.Build("weather", segment.BottomCenter, noOptions{})
	if err == nil {
		t.Fatal("expected an unknown segment to be refused")
	}

	for _, want := range []string{"bottom.center", "weather", "model, scroll"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected %q to mention %q", err, want)
		}
	}
}

func TestASegmentRefusingItsOptionsSaysWhereTheyWereWritten(t *testing.T) {
	refuses := func(segment.Options) (segment.Segment, error) {
		return nil, errors.New("no shouting")
	}

	_, err := segment.Registry{"model": refuses}.Build("model", segment.TopRight, noOptions{})
	if err == nil {
		t.Fatal("expected the refusal to be reported")
	}

	for _, want := range []string{"top.right", "model", "no shouting"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected %q to mention %q", err, want)
		}
	}
}

func TestTheNamesOnOfferAreListedInOrder(t *testing.T) {
	set := segment.Registry{"model": offering(""), "scroll": offering(""), "modes": offering("")}

	got := strings.Join(set.Available(), ",")
	if want := "model,modes,scroll"; got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

type tomlOptions string

type dynamicIdleSegment struct {
	idleInterval time.Duration
}

func (self *dynamicIdleSegment) Render(segment.Context) string  { return "" }
func (self *dynamicIdleSegment) RefreshInterval() time.Duration { return 125 * time.Millisecond }
func (self *dynamicIdleSegment) Persistent() bool               { return true }
func (self *dynamicIdleSegment) IdleRefreshInterval() time.Duration {
	return self.idleInterval
}

func (self tomlOptions) Read(into any) error {
	_, err := toml.Decode(string(self), into)
	return err
}

func TestAChangingIdleIntervalIsReadEachTime(t *testing.T) {
	built := &dynamicIdleSegment{idleInterval: time.Second}
	layout := segment.Layout{segment.BottomLeft: {built}}

	if got := layout.IdleRefreshInterval(); got != time.Second {
		t.Errorf("initial idle interval = %s", got)
	}

	built.idleInterval = 125 * time.Millisecond

	if got := layout.IdleRefreshInterval(); got != 125*time.Millisecond {
		t.Errorf("changed idle interval = %s", got)
	}
}

func TestTheActivitySegmentDrivesTheRedrawTicker(t *testing.T) {
	options := tomlOptions("idle = \"·\"\nframes = [\"*\"]\nrate = \"125ms\"\n")

	built, err := activitySpinner.New(func() (bool, int) { return true, 0 })(options)
	if err != nil {
		t.Fatal(err)
	}

	layout := segment.Layout{segment.BottomLeft: {built}}
	if got := layout.RefreshInterval(); got != 125*time.Millisecond {
		t.Errorf("expected the bar to ask for a redraw every 125ms, got %s", got)
	}
}

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

func stoppedClock(t *testing.T, options segment.Options) segment.Segment {
	t.Helper()

	at := time.Date(2026, time.August, 17, 14, 32, 9, 0, time.UTC)

	built, err := localTime.New(func() time.Time { return at })(options)
	if err != nil {
		t.Fatal(err)
	}

	return built
}

func TestTheClockSegmentTellsTheTimeToTheMinuteByDefault(t *testing.T) {
	built := stoppedClock(t, tomlOptions(""))

	if got := style.Plain(built.Render(segment.Context{})); got != "14:32" {
		t.Errorf("expected the default format to stop at the minute, got %q", got)
	}
}

func TestTheClockSegmentTakesTheFormatItIsGiven(t *testing.T) {
	built := stoppedClock(t, tomlOptions("format = \"15:04:05\"\n"))

	if got := style.Plain(built.Render(segment.Context{})); got != "14:32:09" {
		t.Errorf("expected the given format to be honoured, got %q", got)
	}
}

func TestTheClockSegmentKeepsTheBarTickingBetweenTurns(t *testing.T) {
	layout := segment.Layout{segment.TopRight: {stoppedClock(t, tomlOptions(""))}}

	if got := layout.RefreshInterval(); got != time.Second {
		t.Errorf("expected a redraw every second, got %s", got)
	}

	if got := layout.IdleRefreshInterval(); got != time.Second {
		t.Errorf("expected a clock to be redrawn every second between turns, got %s", got)
	}
}

func TestALayoutWithNothingIdlingSaysSo(t *testing.T) {
	layout := segment.Layout{segment.TopRight: {offeringSegment(t, "gpt")}}

	if got := layout.IdleRefreshInterval(); got != 0 {
		t.Errorf("expected a still bar to be left alone between turns, got %s", got)
	}
}

func offeringSegment(t *testing.T, text string) segment.Segment {
	t.Helper()

	built, err := offering(text)(tomlOptions(""))
	if err != nil {
		t.Fatal(err)
	}

	return built
}

func TestTheSessionEmojiSegmentStandsForTheAnimal(t *testing.T) {
	built, err := sessionEmoji.New("brave-otter")(tomlOptions(""))
	if err != nil {
		t.Fatal(err)
	}

	if got := style.Plain(built.Render(segment.Context{})); got != "🦦" {
		t.Errorf("expected the otter emoji, got %q", got)
	}
}

func TestTheSessionEmojiSegmentDrawsNothingForAnUnknownAnimal(t *testing.T) {
	built, err := sessionEmoji.New("brave-tester")(tomlOptions(""))
	if err != nil {
		t.Fatal(err)
	}

	if got := built.Render(segment.Context{}); got != "" {
		t.Errorf("expected nothing, got %q", got)
	}
}

func TestTheSessionSegmentNamesTheSession(t *testing.T) {
	built, err := sessionName.New("brave-otter")(tomlOptions(""))
	if err != nil {
		t.Fatal(err)
	}

	if got := style.Plain(built.Render(segment.Context{})); got != "brave-otter" {
		t.Errorf("expected the session name, got %q", got)
	}
}

func drawn(t *testing.T, factory segment.Factory) string {
	t.Helper()

	built, err := factory(tomlOptions(""))
	if err != nil {
		t.Fatal(err)
	}

	return style.Plain(built.Render(segment.Context{}))
}

func TestTheTurnCountSegmentCountsFromTheFirstTurn(t *testing.T) {
	if got := drawn(t, turnCount.New(func() int { return 3 })); got != "#3" {
		t.Errorf("expected the third turn to be marked, got %q", got)
	}

	if got := drawn(t, turnCount.New(func() int { return 0 })); got != "" {
		t.Errorf("expected a session with nothing asked of it to say nothing, got %q", got)
	}
}

func timerShowing(t *testing.T, elapsed time.Duration) segment.Segment {
	t.Helper()

	built, err := turnTimer.New(func() time.Duration {
		return elapsed
	})(tomlOptions(""))
	if err != nil {
		t.Fatal(err)
	}

	return built
}

func TestTheTurnTimerCountsInWholeSeconds(t *testing.T) {
	for elapsed, want := range map[time.Duration]string{
		900 * time.Millisecond:               "0s",
		9*time.Second + 400*time.Millisecond: "9s",
		69 * time.Second:                     "1m09s",
		2*time.Hour + 3*time.Minute:          "2h03m",
	} {
		built := timerShowing(t, elapsed)

		if got := style.Plain(built.Render(segment.Context{})); got != want {
			t.Errorf("expected %s to read %q, got %q", elapsed, want, got)
		}
	}
}

func TestTheTurnTimerHoldsItsUnitsBackFromItsNumbers(t *testing.T) {
	built := timerShowing(t, 69*time.Second)

	if got, want := built.Render(segment.Context{}), style.Quantity("1m09s"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTheTurnTimerAsksForASecondlyRedraw(t *testing.T) {
	layout := segment.Layout{segment.BottomLeft: {timerShowing(t, time.Second)}}

	if got := layout.RefreshInterval(); got != time.Second {
		t.Errorf("expected a redraw every second, got %s", got)
	}

	if got := layout.IdleRefreshInterval(); got != time.Second {
		t.Errorf("expected a redraw every second while idle, got %s", got)
	}
}
