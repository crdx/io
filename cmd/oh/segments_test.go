package main

import (
	"testing"
	"time"

	"github.com/BurntSushi/toml"

	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/segment/activeModel"
	"crdx.org/io/cmd/oh/segment/activitySpinner"
	"crdx.org/io/cmd/oh/segment/contextUsage"
	"crdx.org/io/cmd/oh/segment/currentSession"
	"crdx.org/io/cmd/oh/segment/currentTime"
	"crdx.org/io/cmd/oh/segment/gitBranch"
	"crdx.org/io/cmd/oh/segment/lastTps"
	"crdx.org/io/cmd/oh/segment/modeToggle"
	"crdx.org/io/cmd/oh/segment/scrollOverflow"
	"crdx.org/io/cmd/oh/segment/turnCount"
	"crdx.org/io/cmd/oh/segment/turnElapsed"
	"crdx.org/io/cmd/oh/segment/workingDirectory"
)

type goldenSegmentOptions string

func (self goldenSegmentOptions) Read(into any) error {
	_, err := toml.Decode(string(self), into)

	return err
}

func goldenSegmentPass(
	t *testing.T,
	factory segment.Factory,
	options string,
	context segment.Context,
) func() string {
	t.Helper()

	built, err := factory(goldenSegmentOptions(options))
	if err != nil {
		t.Fatal(err)
	}

	return func() string { return built.Render(context) }
}

func goldenBarLayout(t *testing.T, harness *Harness) segment.Layout {
	t.Helper()

	config := configFrom(t, `
		[bar.top]
		left = []
		center = []
		right = [{ segment = "scroll-overflow", direction = "up" }]

		[bar.bottom]
		left = [
			{ segment = "activity-spinner", idle = "✧·", frames = ["✦·", "·✦", "·✧", "✧·"], rate = "125ms" },
			{ segment = "turn-elapsed" },
			{ segment = "mode-toggle" },
			{ segment = "working-directory" },
			{ segment = "git-branch" },
			{ segment = "active-model" },
			{ segment = "context-usage" },
		]
		center = []
		right = [
			{ segment = "turn-count" },
			{ segment = "last-tps" },
			{ segment = "current-session" },
			{ segment = "current-time", format = "15:04" },
			{ segment = "scroll-overflow", direction = "down" },
		]
	`)

	layout, err := config.layout(
		availableSegments(workspaceMarker, "brave-otter", "gpt-5.6-sol", "high", harness),
	)
	if err != nil {
		t.Fatal(err)
	}

	return layout
}

func TestEverySegmentDrawsItsRepresentativeStates(t *testing.T) {
	at := time.Date(2026, time.August, 23, 14, 32, 9, 0, time.UTC)
	spinnerOptions := `
		idle = "✧·"
		frames = ["✦·", "·✦"]
		rate = "125ms"
	`

	passes := map[string]func() string{
		"active-model / medium effort": goldenSegmentPass(
			t,
			activeModel.New("gpt-5.6-sol", "medium"),
			"",
			segment.Context{},
		),
		"activity-spinner / idle": goldenSegmentPass(
			t,
			activitySpinner.New(func() (bool, int) { return false, 0 }),
			spinnerOptions,
			segment.Context{},
		),
		"activity-spinner / running first frame": goldenSegmentPass(
			t,
			activitySpinner.New(func() (bool, int) { return true, 0 }),
			spinnerOptions,
			segment.Context{},
		),
		"activity-spinner / running second frame": goldenSegmentPass(
			t,
			activitySpinner.New(func() (bool, int) { return true, 1 }),
			spinnerOptions,
			segment.Context{},
		),
		"context-usage / known": goldenSegmentPass(
			t,
			contextUsage.New(func() (int, int) { return 182_000, 200_000 }),
			"",
			segment.Context{},
		),
		"context-usage / one million": goldenSegmentPass(
			t,
			contextUsage.New(func() (int, int) { return 500_000, 1_000_000 }),
			"",
			segment.Context{},
		),
		"context-usage / unknown": goldenSegmentPass(
			t,
			contextUsage.New(func() (int, int) { return 0, 0 }),
			"",
			segment.Context{},
		),
		"git-branch / outside a repository": goldenSegmentPass(
			t,
			gitBranch.New(workspaceMarker),
			"",
			segment.Context{},
		),
		"last-tps / a fast turn": goldenSegmentPass(
			t,
			lastTps.New(func() (float64, bool) { return 42.4, true }),
			"",
			segment.Context{},
		),
		"last-tps / a slow turn": goldenSegmentPass(
			t,
			lastTps.New(func() (float64, bool) { return 4.25, true }),
			"",
			segment.Context{},
		),
		"last-tps / nothing yet": goldenSegmentPass(
			t,
			lastTps.New(func() (float64, bool) { return 0, false }),
			"",
			segment.Context{},
		),
		"turn-count / nothing asked yet": goldenSegmentPass(
			t,
			turnCount.New(func() int { return 0 }),
			"",
			segment.Context{},
		),
		"turn-count / third turn": goldenSegmentPass(
			t,
			turnCount.New(func() int { return 3 }),
			"",
			segment.Context{},
		),
		"current-session": goldenSegmentPass(
			t,
			currentSession.New("brave-otter"),
			"",
			segment.Context{},
		),
		"current-time / custom format": goldenSegmentPass(
			t,
			currentTime.New(func() time.Time { return at }),
			`format = "15:04:05"`,
			segment.Context{},
		),
		"current-time / default format": goldenSegmentPass(
			t,
			currentTime.New(func() time.Time { return at }),
			"",
			segment.Context{},
		),
		"mode-toggle / all granted": goldenSegmentPass(
			t,
			modeToggle.New(caps.All, func() bool { return false }),
			"",
			segment.Context{},
		),
		"mode-toggle / pending chord": goldenSegmentPass(
			t,
			modeToggle.New(func() caps.Set { return caps.Read }, func() bool { return true }),
			"",
			segment.Context{},
		),
		"mode-toggle / read only": goldenSegmentPass(
			t,
			modeToggle.New(func() caps.Set { return caps.Read }, func() bool { return false }),
			"",
			segment.Context{},
		),
		"scroll-overflow / down": goldenSegmentPass(
			t,
			scrollOverflow.New,
			`direction = "down"`,
			segment.Context{HiddenLinesBelow: 7},
		),
		"scroll-overflow / empty": goldenSegmentPass(
			t,
			scrollOverflow.New,
			`direction = "up"`,
			segment.Context{},
		),
		"scroll-overflow / up": goldenSegmentPass(
			t,
			scrollOverflow.New,
			`direction = "up"`,
			segment.Context{HiddenLinesAbove: 3},
		),
		"turn-elapsed / idle": goldenSegmentPass(
			t,
			turnElapsed.New(func() (bool, time.Duration) { return false, 69 * time.Second }),
			"",
			segment.Context{},
		),
		"turn-elapsed / running": goldenSegmentPass(
			t,
			turnElapsed.New(func() (bool, time.Duration) { return true, 69 * time.Second }),
			"",
			segment.Context{},
		),
		"working-directory": goldenSegmentPass(
			t,
			workingDirectory.New("/workspace/project"),
			"",
			segment.Context{},
		),
	}

	compareWithGolden(t, "segments", ".ansi", passes)
	compareWithGolden(t, "segments", ".screen", shownPasses(t, passes))
}
