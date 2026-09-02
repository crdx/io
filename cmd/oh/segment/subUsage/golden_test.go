package subUsage

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/style"
)

var updateGoldens = flag.Bool("update", false, "write what was drawn back to the golden files")

func window(duration time.Duration, percent float64, remainingTime time.Duration) agent.UsageWindow {
	return agent.UsageWindow{
		Duration: duration,
		Percent:  percent,
		ResetsAt: testNow.Add(remainingTime),
	}
}

func scoped(scope string, percent float64) agent.UsageWindow {
	built := window(5*time.Hour, percent, 2*time.Hour)
	built.Scope = scope

	return built
}

type segmentCase struct {
	name      string
	modelName string
	windows   []agent.UsageWindow
	status    usageStatus
	failure   string
	fetchedAt time.Time
}

func segmentCases() []segmentCase {
	return []segmentCase{
		{
			name:    "windows on an even burn",
			windows: []agent.UsageWindow{window(5*time.Hour, 40, 150*time.Minute), window(7*24*time.Hour, 12, 6*24*time.Hour)},
		},
		{name: "a window ahead of its pace", windows: []agent.UsageWindow{window(5*time.Hour, 28, 4*time.Hour)}},
		{name: "a window far ahead of its pace", windows: []agent.UsageWindow{window(5*time.Hour, 60, 4*time.Hour)}},
		{name: "a window near its limit", windows: []agent.UsageWindow{window(5*time.Hour, 95, time.Hour)}},
		{
			name:      "a scoped window governing this model",
			modelName: "gpt-5.3-codex-spark",
			windows:   []agent.UsageWindow{window(5*time.Hour, 40, 150*time.Minute), scoped("gpt-5.3-codex-spark", 70)},
		},
		{
			name:      "a scoped window governing another model",
			modelName: "claude-sonnet-4-6",
			windows:   []agent.UsageWindow{window(5*time.Hour, 40, 150*time.Minute), scoped("opus", 70)},
		},
		{
			name:    "a window that is over its limit",
			windows: []agent.UsageWindow{{Duration: 30 * 24 * time.Hour, Percent: 100, IsLimited: true}},
		},
		{name: "a window whose reset has passed", windows: []agent.UsageWindow{window(5*time.Hour, 40, -time.Minute)}},
		{
			name:    "a window with no reset at all",
			windows: []agent.UsageWindow{{Duration: 5 * time.Hour, Percent: 40}},
		},
		{
			name:    "a fetch under way",
			windows: []agent.UsageWindow{window(5*time.Hour, 40, 150*time.Minute)},
			status:  usageFetching,
		},
		{name: "nothing fetched yet", status: usagePending},
		{
			name:    "a fetch that was refused",
			windows: []agent.UsageWindow{window(5*time.Hour, 40, 150*time.Minute)},
			status:  usageRetrying,
			failure: "429",
		},
		{name: "nothing to show at all"},
	}
}

func drawEachCase(t *testing.T, isPlain bool) string {
	t.Helper()

	var drawn strings.Builder

	for _, test := range segmentCases() {
		clock := &testClock{now: testNow}

		segment := &state{
			modelName: strings.ToLower(test.modelName),
			rate:      defaultRate,
			now:       clock.read,
			status:    test.status,
		}

		fetchedAt := test.fetchedAt
		if fetchedAt.IsZero() {
			fetchedAt = testNow
		}

		text := segment.draw(snapshot{
			windows:   test.windows,
			fetchedAt: fetchedAt,
			status:    test.status,
			failure:   test.failure,
		})

		if isPlain {
			text = style.Plain(text)
		}

		drawn.WriteString("=== ")
		drawn.WriteString(test.name)
		drawn.WriteString(" ===\n")
		drawn.WriteString(text)
		drawn.WriteString("\n")
	}

	return drawn.String()
}

func TestEverySegmentMatchesTheGolden(t *testing.T) {
	checkGolden(t, "segment.txt", drawEachCase(t, true))
}

func TestEveryStyledSegmentMatchesTheGolden(t *testing.T) {
	checkGolden(t, "segment.ansi", drawEachCase(t, false))
}

func TestEveryGoldenIsClaimedByATest(t *testing.T) {
	claimed := map[string]struct{}{
		"segment.txt":  {},
		"segment.ansi": {},
	}

	entries, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatal(err)
	}

	for _, entry := range entries {
		if _, isClaimed := claimed[entry.Name()]; !isClaimed {
			t.Errorf("nothing draws testdata/%s", entry.Name())
		}

		delete(claimed, entry.Name())
	}

	for name := range claimed {
		t.Errorf("testdata/%s was never written", name)
	}
}

func checkGolden(t *testing.T, name string, drawn string) {
	t.Helper()

	goldenPath := filepath.Join("testdata", name)

	if *updateGoldens {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o700); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(goldenPath, []byte(drawn), 0o600); err != nil {
			t.Fatal(err)
		}

		return
	}

	want, err := os.ReadFile(goldenPath) //nolint:gosec // fixed testdata path
	if err != nil {
		t.Fatal(err)
	}

	if drawn != string(want) {
		t.Errorf("what was drawn differs from %s\n--- got ---\n%s--- want ---\n%s", goldenPath, drawn, want)
	}
}
