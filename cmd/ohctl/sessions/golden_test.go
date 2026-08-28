package sessions

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"crdx.org/io/cmd/oh/location"
	"crdx.org/io/cmd/ohctl/console"
)

var updateGoldens = flag.Bool("update", false, "write what was drawn back to the golden files")

func TestUsageMatchesTheGolden(t *testing.T) {
	assertGolden(t, "usage.txt", strings.ReplaceAll(usage, "$0", "ohctl"))
}

func TestTheListingMatchesTheGolden(t *testing.T) {
	var drawn strings.Builder
	if err := writeTable(goldenListings(time.Now()), &drawn); err != nil {
		t.Fatal(err)
	}

	assertGolden(t, "listing.txt", drawn.String())
}

func TestTheJSONListingMatchesTheGolden(t *testing.T) {
	var written strings.Builder
	fixed := time.Date(2026, time.August, 28, 14, 15, 25, 0, time.UTC)
	if err := writeJSON(goldenListings(fixed), &written); err != nil {
		t.Fatal(err)
	}

	assertGolden(t, "listing.json", written.String())
}

func TestAnEmptyListingMatchesTheGolden(t *testing.T) {
	t.Setenv(location.StateDirVariable, t.TempDir())

	var screen, failure strings.Builder
	output := console.Output{Screen: &screen, Failure: &failure}
	if err := run(&inputOpts{}, output); err != nil {
		t.Fatal(err)
	}
	if err := run(&inputOpts{JSON: true}, output); err != nil {
		t.Fatal(err)
	}

	assertGolden(t, "empty.txt", strings.Join([]string{
		"=== screen ===\n", screen.String(),
		"=== failure ===\n", failure.String(),
	}, ""))
}

func goldenListings(now time.Time) []Listing {
	return []Listing{
		{
			Name:         "wild-scorpion",
			Status:       runningStatus,
			IsRunning:    true,
			Title:        "audit-golden-files 🟡",
			WorkspaceDir: "/home/agent/proj/io",
			ScratchDir:   "/state/tmps/wild-scorpion",
			SessionDir:   "/state/sessions/wild-scorpion",
			Model:        "claude-opus-5",
			Effort:       "medium",
			Messages:     32,
			Started:      now.Add(-7 * time.Hour),
			Touched:      now.Add(-3 * time.Hour),
		},
		{
			Name:         "dewy-vole",
			Status:       endedStatus,
			Title:        strings.Repeat("a-title-far-wider-than-its-column ", 3),
			WorkspaceDir: "/home/agent/proj/io",
			ScratchDir:   "/state/tmps/dewy-vole",
			SessionDir:   "/state/sessions/dewy-vole",
			Model:        "gpt-5.3-codex",
			Effort:       "high",
			Messages:     8,
			Started:      now.Add(-90 * time.Minute),
			Touched:      now.Add(-30 * time.Minute),
		},
		{
			Name:         "chewy-raven",
			Status:       endedStatus,
			WorkspaceDir: "/home/agent/.system",
			ScratchDir:   "/state/tmps/chewy-raven",
			SessionDir:   "/state/sessions/chewy-raven",
			Started:      now,
			Touched:      now,
		},
	}
}

func assertGolden(t *testing.T, name string, drawn string) {
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
		t.Errorf("output differs from %s\n--- got ---\n%s--- want ---\n%s", goldenPath, drawn, want)
	}
}
