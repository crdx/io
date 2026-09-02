package usage

import (
	"strings"
	"testing"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/oh/width"
)

func drawnReport(t *testing.T, at time.Time) string {
	t.Helper()

	scoped := agent.UsageWindow{
		Duration: 5 * time.Hour,
		Percent:  8,
		ResetsAt: collectedAt.Add(2 * time.Hour),
		Scope:    "gpt-5.3-codex-spark",
	}

	report := Collect(t.Context(), []Source{
		{
			Provider: "codex",
			Label:    "OpenAI",
			Reporter: &scriptedReporter{windows: []agent.UsageWindow{sessionWindow(70), weeklyWindow(20), scoped}},
		},
		{
			Provider:             "anthropic",
			Label:                "Anthropic",
			Reporter:             &scriptedReporter{windows: []agent.UsageWindow{weeklyWindow(50)}},
			HasIdleSessionWindow: true,
		},
	}, nowAt(collectedAt))

	return style.Plain(Render(report, at, nil))
}

func TestEveryGaugeIsDrawnAgainstTheSameColumn(t *testing.T) {
	drawn := drawnReport(t, collectedAt.Add(time.Minute))

	want := strings.Join([]string{
		"● OpenAI",
		"Session        70% █████████┃█░░░░░ ▲ 10  1h59m",
		"Week           20% ███░░░░░░░░┃░░░░ ▼ 51  1d23h",
		"Spark Session   8% █░░░░░░░░┃░░░░░░ ▼ 52  1h59m",
		"",
		"● Anthropic",
		"Session         0% ░░░░░░░░░░░░░░░░       idle",
		"Week           50% ████████░░░┃░░░░ ▼ 21  1d23h",
		"",
	}, "\n")

	if drawn != want {
		t.Errorf("drew\n%s\nwant\n%s", drawn, want)
	}
}

func TestTheBulletFollowsTheFreshnessOfTheSnapshot(t *testing.T) {
	for _, test := range []struct {
		freshness        string
		age              time.Duration
		isSelfRefreshing bool
		mark             string
		style            style.Style
		isAgeWorthSayng  bool
	}{
		{freshness: FreshnessFresh, age: time.Minute, mark: freshMark, style: style.Success},
		{freshness: FreshnessDue, age: 10 * time.Minute, mark: freshMark, style: style.Change, isAgeWorthSayng: true},
		{
			freshness:        FreshnessStale,
			age:              45 * time.Minute,
			isSelfRefreshing: true,
			mark:             freshMark,
			style:            style.Failure,
			isAgeWorthSayng:  true,
		},
		{
			freshness:       FreshnessWaiting,
			age:             45 * time.Minute,
			mark:            waitingMark,
			style:           style.Change,
			isAgeWorthSayng: true,
		},
	} {
		t.Run(test.freshness, func(t *testing.T) {
			snapshot := Snapshot{
				FreshWithinSeconds: 360,
				StaleAfterSeconds:  1800,
				IsSelfRefreshing:   test.isSelfRefreshing,
			}

			if got := snapshot.FreshnessAt(test.age); got != test.freshness {
				t.Errorf("read the freshness as %q", got)
			}

			mark, appearance, isAgeWorthSaying := freshness(test.age, snapshot)
			if mark != test.mark {
				t.Errorf("drew the mark as %q", mark)
			}

			if got := appearance(mark); got != test.style(mark) {
				t.Errorf("drew the bullet as %q", got)
			}

			if isAgeWorthSaying != test.isAgeWorthSayng {
				t.Errorf("said the age: %t", isAgeWorthSaying)
			}
		})
	}
}

func TestAProviderThatCouldNotBeReachedSaysWhy(t *testing.T) {
	report := Report{Providers: []Snapshot{{
		Provider: Provider{ID: "anthropic", Label: "Anthropic"},
		Status:   StatusFailed,
		Message:  "refused with 429",
	}}}

	drawn := style.Plain(Render(report, collectedAt, nil))

	if drawn != "✖ Anthropic refused with 429\n" {
		t.Errorf("drew %q", drawn)
	}
}

func TestAnEmptyReportSaysThereIsNothingToShow(t *testing.T) {
	drawn := style.Plain(Render(Report{}, collectedAt, nil))

	if drawn != "no usage to report\n" {
		t.Errorf("drew %q", drawn)
	}
}

func TestACountdownLeadsWithItsLargestUnit(t *testing.T) {
	for _, test := range []struct {
		remainingTime time.Duration
		want          string
	}{
		{remainingTime: 45 * time.Second, want: "45s"},
		{remainingTime: 90 * time.Second, want: "1m30s"},
		{remainingTime: 3 * time.Hour, want: "3h00m"},
		{remainingTime: 75*time.Hour + 57*time.Minute, want: "3d03h"},
	} {
		t.Run(test.want, func(t *testing.T) {
			if got := style.Plain(countdown(test.remainingTime)); got != test.want {
				t.Errorf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestACountdownDimsTheUnitAfterItsFirst(t *testing.T) {
	drawn := countdown(75*time.Hour + 57*time.Minute)

	if want := style.Normal("3d") + style.Dim("03h"); drawn != want {
		t.Errorf("drew %q, want %q", drawn, want)
	}
}

func TestADrawnGaugeTakesExactlyTheCellsItWasGiven(t *testing.T) {
	expected := 40
	limit := Limit{UsedPercent: 62, ExpectedPercent: &expected}

	drawn, isDrawn := gaugePlacement(limit, PaceAhead, gaugeWidth, Graphics{CellWidth: 9, CellHeight: 18})
	if !isDrawn {
		t.Fatal("the gauge was not drawn")
	}

	if got := width.Of(drawn); got != gaugeWidth {
		t.Errorf("the gauge occupies %d cells, want %d", got, gaugeWidth)
	}

	if got := style.Plain(drawn); width.Of(got) != gaugeWidth {
		t.Errorf("the placeholder run reads %q", got)
	}
}

func TestADrawnRowStillLinesUpWithTheRestOfTheReport(t *testing.T) {
	report := Collect(t.Context(), []Source{{
		Provider: "codex",
		Label:    "OpenAI",
		Reporter: &scriptedReporter{windows: []agent.UsageWindow{sessionWindow(70), weeklyWindow(20)}},
	}}, nowAt(collectedAt))

	drawing := &Graphics{CellWidth: 9, CellHeight: 18}

	for _, line := range strings.Split(strings.TrimSuffix(Render(report, collectedAt, drawing), "\n"), "\n")[1:] {
		if got := width.Of(line); got != width.Of(style.Plain(line)) {
			t.Errorf("the line %q measures %d", line, got)
		}
	}
}
