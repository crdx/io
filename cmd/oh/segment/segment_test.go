package segment_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
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
	"crdx.org/io/cmd/oh/turn"
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

type refreshingSegment struct {
	after time.Duration
}

func (self *refreshingSegment) Render(segment.Context) string { return "" }

func (self *refreshingSegment) NextRefresh(phase segment.Phase) time.Time {
	if self.after == 0 {
		return time.Time{}
	}

	return phase.At.Add(self.after)
}

func (self tomlOptions) Read(into any) error {
	_, err := toml.Decode(string(self), into)
	return err
}

func TestTheLayoutIsRedrawnForWhicheverSegmentChangesSoonest(t *testing.T) {
	at := time.Date(2026, time.August, 17, 14, 32, 9, 0, time.UTC)

	layout := segment.Layout{
		segment.BottomLeft:  {&refreshingSegment{after: time.Second}, offeringSegment(t, "gpt")},
		segment.BottomRight: {&refreshingSegment{after: 125 * time.Millisecond}},
	}

	if got := layout.NextRefresh(segment.Phase{At: at}); !got.Equal(at.Add(125 * time.Millisecond)) {
		t.Errorf("expected the soonest of the two, got %s", got.Sub(at))
	}
}

func TestALayoutWithNothingToSayIsNeverRedrawn(t *testing.T) {
	at := time.Date(2026, time.August, 17, 14, 32, 9, 0, time.UTC)

	layout := segment.Layout{
		segment.TopRight: {offeringSegment(t, "gpt"), &refreshingSegment{}},
	}

	if got := layout.NextRefresh(segment.Phase{At: at}); !got.IsZero() {
		t.Errorf("expected a still bar to be left alone, got %s", got)
	}
}

func TestAChangingRefreshIsAskedForAfresh(t *testing.T) {
	at := time.Date(2026, time.August, 17, 14, 32, 9, 0, time.UTC)

	built := &refreshingSegment{after: time.Second}
	layout := segment.Layout{segment.BottomLeft: {built}}

	if got := layout.NextRefresh(segment.Phase{At: at}); !got.Equal(at.Add(time.Second)) {
		t.Errorf("initial refresh = %s", got.Sub(at))
	}

	built.after = 125 * time.Millisecond

	if got := layout.NextRefresh(segment.Phase{At: at}); !got.Equal(at.Add(125 * time.Millisecond)) {
		t.Errorf("changed refresh = %s", got.Sub(at))
	}
}

func spinnerAt(t *testing.T, at time.Time, isRunning bool) segment.Segment {
	t.Helper()

	options := tomlOptions("idle = \"·\"\nframes = [\"*\", \"+\"]\nrate = \"125ms\"\n")

	built, err := activitySpinner.New(
		func() bool { return isRunning },
		func() time.Time { return at },
	)(options)
	if err != nil {
		t.Fatal(err)
	}

	return built
}

func TestTheActivitySegmentAsksForTheMomentItsFrameTurnsOver(t *testing.T) {
	at := time.Date(2026, time.August, 17, 14, 32, 9, 40*int(time.Millisecond), time.UTC)

	layout := segment.Layout{segment.BottomLeft: {spinnerAt(t, at, true)}}

	want := time.Date(2026, time.August, 17, 14, 32, 9, 125*int(time.Millisecond), time.UTC)
	if got := layout.NextRefresh(segment.Phase{At: at, IsRunning: true}); !got.Equal(want) {
		t.Errorf("expected the next frame at %s, got %s", want, got)
	}
}

func TestTheActivitySegmentStandsStillBetweenTurns(t *testing.T) {
	at := time.Date(2026, time.August, 17, 14, 32, 9, 0, time.UTC)

	layout := segment.Layout{segment.BottomLeft: {spinnerAt(t, at, false)}}

	if got := layout.NextRefresh(segment.Phase{At: at}); !got.IsZero() {
		t.Errorf("expected an idle spinner to ask for nothing, got %s", got)
	}
}

func TestTheActivitySegmentTurnsItsFramesByTheClock(t *testing.T) {
	at := time.Date(2026, time.August, 17, 14, 32, 9, 0, time.UTC)

	first := style.Plain(spinnerAt(t, at, true).Render(segment.Context{}))
	next := style.Plain(spinnerAt(t, at.Add(125*time.Millisecond), true).Render(segment.Context{}))

	if first == next {
		t.Errorf("expected the frame to turn over with the clock, got %q twice", first)
	}

	again := style.Plain(spinnerAt(t, at.Add(250*time.Millisecond), true).Render(segment.Context{}))
	if again != first {
		t.Errorf("expected the frames to come round again, got %q then %q", first, again)
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

func TestTheBranchSegmentLooksAgainAtItsOwnRate(t *testing.T) {
	built, err := gitBranch.New(t.TempDir())(tomlOptions("rate = \"2s\"\n"))
	if err != nil {
		t.Fatal(err)
	}

	layout := segment.Layout{segment.BottomLeft: {built}}
	at := time.Now()

	if got := layout.NextRefresh(segment.Phase{At: at}); !got.Equal(at) {
		t.Errorf("expected a branch never read to be read at once, got %s", got.Sub(at))
	}

	built.Render(segment.Context{})

	readAt := time.Now()
	if got := layout.NextRefresh(segment.Phase{At: readAt}); got.Sub(readAt) > 2*time.Second {
		t.Errorf("expected the given rate to pace the next read, got %s", got.Sub(readAt))
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

func TestTheClockSegmentIsRedrawnOnlyWhenItsFaceChanges(t *testing.T) {
	at := time.Date(2026, time.August, 17, 14, 32, 9, 0, time.UTC)

	toTheMinute := segment.Layout{segment.TopRight: {stoppedClock(t, tomlOptions(""))}}

	want := time.Date(2026, time.August, 17, 14, 33, 0, 0, time.UTC)
	if got := toTheMinute.NextRefresh(segment.Phase{At: at}); !got.Equal(want) {
		t.Errorf("expected a clock reading 14:32 to wait for 14:33, got %s", got)
	}

	toTheSecond := segment.Layout{
		segment.TopRight: {stoppedClock(t, tomlOptions("format = \"15:04:05\"\n"))},
	}

	want = time.Date(2026, time.August, 17, 14, 32, 10, 0, time.UTC)
	if got := toTheSecond.NextRefresh(segment.Phase{At: at}); !got.Equal(want) {
		t.Errorf("expected a clock reading 14:32:09 to wait for 14:32:10, got %s", got)
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

func timerShowing(t *testing.T, timing turn.Timing, isTurnRunning bool) segment.Segment {
	t.Helper()

	built, err := turnTimer.New(func() turn.Timing {
		return timing
	}, func() bool {
		return isTurnRunning
	})(tomlOptions(""))
	if err != nil {
		t.Fatal(err)
	}

	return built
}

func TestTheTurnTimerShowsBothTurnsInWholeMinutes(t *testing.T) {
	tests := []struct {
		timing        turn.Timing
		isTurnRunning bool
		want          string
	}{
		{want: "0m/0m"},
		{timing: turn.Timing{UserTurn: 3 * time.Minute, ModelTurn: time.Minute}, want: "3m/1m"},
		{timing: turn.Timing{UserTurn: 3 * time.Minute}, isTurnRunning: true, want: "3m/0m"},
		{timing: turn.Timing{UserTurn: 12*time.Minute + 59*time.Second, ModelTurn: 3*time.Minute + 59*time.Second}, want: "12m/3m"},
	}

	for _, test := range tests {
		built := timerShowing(t, test.timing, test.isTurnRunning)

		if got := style.Plain(built.Render(segment.Context{})); got != test.want {
			t.Errorf("expected %+v to read %q, got %q", test.timing, test.want, got)
		}
	}
}

func TestTheTurnTimerDimsEverythingExceptTheActiveNumber(t *testing.T) {
	timing := turn.Timing{UserTurn: 3 * time.Minute, ModelTurn: time.Minute}
	tests := []struct {
		isTurnRunning bool
		want          string
	}{
		{want: style.Normal("3") + style.Faint("m") + style.Faint("/") + style.Faint("1m")},
		{isTurnRunning: true, want: style.Faint("3m") + style.Faint("/") + style.Normal("1") + style.Faint("m")},
	}

	for _, test := range tests {
		if got := timerShowing(t, timing, test.isTurnRunning).Render(segment.Context{}); got != test.want {
			t.Errorf("got %q, want %q", got, test.want)
		}
	}
}

func TestTheTurnTimerAsksForTheMomentItsActiveMinuteTurnsOver(t *testing.T) {
	at := time.Date(2026, time.August, 17, 14, 32, 9, 0, time.UTC)

	tests := []struct {
		timing        turn.Timing
		isTurnRunning bool
		want          time.Duration
	}{
		{timing: turn.Timing{UserTurn: 3 * time.Second}, want: 57 * time.Second},
		{timing: turn.Timing{UserTurn: 3*time.Second + 400*time.Millisecond}, want: 56*time.Second + 600*time.Millisecond},
		{timing: turn.Timing{ModelTurn: 69*time.Second + 999*time.Millisecond}, isTurnRunning: true, want: 50*time.Second + time.Millisecond},
		{timing: turn.Timing{ModelTurn: 2*time.Hour + 250*time.Millisecond}, isTurnRunning: true, want: 59*time.Second + 750*time.Millisecond},
	}

	for _, test := range tests {
		layout := segment.Layout{segment.BottomLeft: {timerShowing(t, test.timing, test.isTurnRunning)}}

		phase := segment.Phase{At: at, IsRunning: test.isTurnRunning}
		if got := layout.NextRefresh(phase).Sub(at); got != test.want {
			t.Errorf("expected %+v to be redrawn in %s, got %s", test.timing, test.want, got)
		}
	}
}

func TestABusyBarDrawsEveryMinuteOfATurnExactlyOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		startedAt := time.Now()

		built, err := turnTimer.New(func() turn.Timing {
			return turn.Timing{ModelTurn: time.Since(startedAt)}
		}, func() bool {
			return true
		})(tomlOptions(""))
		if err != nil {
			t.Fatal(err)
		}

		layout := segment.Layout{
			segment.BottomLeft:  {built},
			segment.BottomRight: {stoppedClock(t, tomlOptions(""))},
		}

		var drawn []string

		for pass := range 90 {
			at := layout.NextRefresh(segment.Phase{At: time.Now(), IsRunning: true})
			time.Sleep(time.Until(at))
			drawn = append(drawn, style.Plain(built.Render(segment.Context{})))
			time.Sleep(time.Duration(1+(pass*37)%40) * time.Millisecond)
		}

		for index, got := range drawn {
			want := fmt.Sprintf("0m/%dm", index+1)
			if got != want {
				t.Fatalf("draw %d read %q, want %q", index, got, want)
			}
		}
	})
}

func TestTheTurnTimerKeepsCountingBetweenTurns(t *testing.T) {
	at := time.Date(2026, time.August, 17, 14, 32, 9, 0, time.UTC)
	layout := segment.Layout{segment.BottomLeft: {timerShowing(t, turn.Timing{UserTurn: 3 * time.Second}, false)}}

	if got := layout.NextRefresh(segment.Phase{At: at}); !got.Equal(at.Add(57 * time.Second)) {
		t.Errorf("expected the timer to keep counting while idle, got %s", got.Sub(at))
	}
}
