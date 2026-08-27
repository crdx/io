package picker

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var updateGoldens = flag.Bool("update", false, "write what was drawn back to the golden files")

func TestPickerMatchesTheGolden(t *testing.T) {
	now := time.Now()
	sessions := []*Session{
		{
			Name:         "chewy-sardine",
			Title:        "why does the spinner stutter when a tool runs",
			MessageCount: 12,
			Started:      now.Add(-8 * time.Minute),
			Touched:      now,
			IsRunning:    true,
		},
		{
			Name:         "thick-poodle",
			Title:        "add support for reasoning traces",
			MessageCount: 4,
			Started:      now.Add(-127 * time.Minute),
			Touched:      now.Add(-37 * time.Minute),
		},
		{
			Name:         "funny-badger",
			Title:        "the cancelled turn leaves a tool call unanswered\nand the next request fails",
			MessageCount: 148,
			Started:      now.Add(-8 * time.Hour),
			Touched:      now.Add(-5 * time.Hour),
		},
		{
			Name:         "able-dolphin",
			MessageCount: 1,
			Started:      now.Add(-77 * time.Hour),
			Touched:      now.Add(-73 * time.Hour),
		},
		{
			Name:         "brave-otter",
			Title:        "rename the harness to oh",
			MessageCount: 26,
			Started:      now.Add(-330 * time.Hour),
			Touched:      now.Add(-300 * time.Hour),
		},
	}

	var output strings.Builder
	for i, room := range []int{80, 46} {
		if i > 0 {
			_, _ = fmt.Fprintln(&output)
		}
		_, _ = fmt.Fprintf(&output, "--- %d columns ---\n", room)
		_, _ = fmt.Fprintln(&output, clip(header("/workspace"), room))
		_, _ = fmt.Fprintln(&output)
		_, _ = fmt.Fprintln(&output, columnHeader(room))
		for rowIndex, storedSession := range sessions {
			_, _ = fmt.Fprintln(&output, row(storedSession, rowIndex == 1, room))
		}
	}

	goldenPath := filepath.Join("testdata", "picker.golden")
	if *updateGoldens {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, []byte(output.String()), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	want, err := os.ReadFile(goldenPath) //nolint:gosec // fixed testdata path
	if err != nil {
		t.Fatal(err)
	}
	if output.String() != string(want) {
		t.Errorf("picker differs from %s\n--- got ---\n%s--- want ---\n%s", goldenPath, output.String(), want)
	}
}
