package picker

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"crdx.org/io/cmd/oh/menu"
	"crdx.org/io/internal/util/strutil"
)

var updateGoldens = flag.Bool("update", false, "write what was drawn back to the golden files")

func storedSessions() []*Session {
	now := time.Now()

	return []*Session{
		{
			Name:         "chewy-sardine",
			Title:        "why does the spinner stutter when a tool runs",
			Model:        "Codex 5.3",
			ModelID:      "gpt-5.3-codex",
			Effort:       "high",
			MessageCount: 12,
			StartedAt:    now.Add(-8 * time.Minute),
			TouchedAt:    now,
			IsRunning:    true,
			IsFast:       true,
		},
		{
			Name:         "thick-poodle",
			Title:        "add support for reasoning traces",
			Model:        "Sonnet 5",
			ModelID:      "claude-sonnet-5",
			Effort:       "medium",
			MessageCount: 4,
			StartedAt:    now.Add(-127 * time.Minute),
			TouchedAt:    now.Add(-37 * time.Minute),
		},
		{
			Name:         "funny-badger",
			Model:        "Qwen Coder 3 30B",
			ModelID:      "qwen3-coder:30b-a3b-instruct",
			Effort:       "medium",
			Title:        "the cancelled turn leaves a tool call unanswered\nand the next request fails",
			MessageCount: 148,
			StartedAt:    now.Add(-8 * time.Hour),
			TouchedAt:    now.Add(-5 * time.Hour),
		},
		{
			Name:         "able-dolphin",
			MessageCount: 1,
			StartedAt:    now.Add(-77 * time.Hour),
			TouchedAt:    now.Add(-73 * time.Hour),
		},
		{
			Name:         "brave-otter",
			Title:        "rename the harness to oh",
			Model:        "Codex 5.3",
			ModelID:      "gpt-5.3-codex",
			MessageCount: 26,
			StartedAt:    now.Add(-330 * time.Hour),
			TouchedAt:    now.Add(-300 * time.Hour),
		},
	}
}

func compareWithGolden(t *testing.T, name string, drawn string) {
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
		t.Errorf("rows differ from %s\n--- got ---\n%s--- want ---\n%s", goldenPath, drawn, want)
	}
}

func TestTheRowsOfTheSessionPickerMatchTheGolden(t *testing.T) {
	sessions := &sessionList{sessions: storedSessions()}

	var output strings.Builder

	for i, room := range []int{150, 120, 80, 46} {
		if i > 0 {
			_, _ = fmt.Fprintln(&output)
		}
		_, _ = fmt.Fprintf(&output, "--- %d columns ---\n", room)
		_, _ = fmt.Fprintln(&output, sessions.ColumnHeader(room))
		for rowIndex, storedSession := range sessions.sessions {
			_, _ = fmt.Fprintln(&output, row(storedSession, rowIndex == 1, room))
		}
	}

	compareWithGolden(t, "rows.golden", output.String())
}

func TestWhatTheSessionPickerPaintsMatchesTheGolden(t *testing.T) {
	frames := []struct {
		name   string
		room   int
		height int
		cursor int
		query  string
	}{
		{name: "a wide terminal, where the title has the room", room: 150, height: 24, cursor: 1},
		{name: "a terminal wide enough for the model that answered", room: 120, height: 24, cursor: 1},
		{name: "no room for the model, so the room goes to the title", room: 80, height: 24, cursor: 3},
		{name: "a narrow terminal, where the columns are clipped", room: 46, height: 24, cursor: 1},
		{name: "a filter narrowing the list to the model that answered", room: 120, height: 24, cursor: 0, query: "codex"},
		{name: "a filter matching the mode a session ran in", room: 120, height: 24, cursor: 0, query: "fast"},
		{name: "a filter no session answers to", room: 120, height: 24, cursor: 0, query: "kimi"},
	}

	var output strings.Builder

	for _, frame := range frames {
		fmt.Fprintf(&output, "=== %s ===\n%s\n", frame.name, strutil.VisibleEscapes(
			menu.Paint(
				&sessionList{sessions: storedSessions()},
				frame.room,
				frame.height,
				frame.cursor,
				frame.query,
			),
		))
	}

	compareWithGolden(t, "painted.ansi", output.String())
}
