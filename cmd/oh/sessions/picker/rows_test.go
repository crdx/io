package picker

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"crdx.org/io/cmd/oh/key"
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
			IsFast:       true,
		},
		{
			Name:         "funny-badger",
			Model:        "Qwen Coder 3 30B Instruct",
			ModelID:      "qwen3-coder:30b-a3b-instruct",
			Effort:       "medium",
			IsFast:       true,
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

func archivedSessions() []*Session {
	now := time.Now()

	return []*Session{
		{
			Name:         "brave-otter",
			Title:        "rename the harness to oh",
			Model:        "Codex 5.3",
			ModelID:      "gpt-5.3-codex",
			MessageCount: 26,
			StartedAt:    now.Add(-330 * time.Hour),
			TouchedAt:    now.Add(-300 * time.Hour),
			IsArchived:   true,
		},
		{
			Name:         "wiry-turtle",
			Title:        "add an archive view to the picker",
			Model:        "Sonnet 5",
			ModelID:      "claude-sonnet-5",
			Effort:       "high",
			MessageCount: 61,
			StartedAt:    now.Add(-52 * time.Hour),
			TouchedAt:    now.Add(-49 * time.Hour),
			IsArchived:   true,
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
	sessions := &sessionList{store: Store{Sessions: storedSessions()}}

	var output strings.Builder

	for i, room := range []int{150, 120, 80, 46} {
		if i > 0 {
			_, _ = fmt.Fprintln(&output)
		}
		_, _ = fmt.Fprintf(&output, "--- %d columns ---\n", room)
		_, _ = fmt.Fprintln(&output, sessions.ColumnHeader(room))
		for rowIndex, storedSession := range sessions.rows() {
			_, _ = fmt.Fprintln(&output, row(storedSession, rowIndex == 1, room))
		}
	}

	compareWithGolden(t, "rows.golden", output.String())
}

func TestWhatTheSessionPickerPaintsMatchesTheGolden(t *testing.T) {
	frames := []struct {
		name       string
		room       int
		height     int
		cursor     int
		query      string
		removalKey *key.Key

		isArchivedView     bool
		hasNothingArchived bool
	}{
		{name: "a wide terminal, where the title has the room", room: 150, height: 24, cursor: 1},
		{name: "a terminal wide enough for the model that answered", room: 120, height: 24, cursor: 1},
		{name: "no room for the model, so the room goes to the title", room: 80, height: 24, cursor: 3},
		{name: "a narrow terminal, where the columns are clipped", room: 46, height: 24, cursor: 1},
		{name: "a filter narrowing the list to the model that answered", room: 120, height: 24, cursor: 0, query: "codex"},
		{name: "a filter matching the mode a session ran in", room: 120, height: 24, cursor: 0, query: "fast"},
		{name: "a filter no session answers to", room: 120, height: 24, cursor: 0, query: "kimi"},
		{name: "the confirmation asked before a session is archived", room: 120, height: 24, cursor: 1, removalKey: new(archiveKeypress())},
		{name: "the confirmation in a narrow terminal", room: 46, height: 24, cursor: 1, removalKey: new(archiveKeypress())},
		{name: "the confirmation taking the place of a filter being typed", room: 120, height: 24, cursor: 1, query: "codex", removalKey: new(archiveKeypress())},
		{name: "a running session under the cursor, which is never offered for archiving", room: 120, height: 24, cursor: 0, query: "codex", removalKey: new(archiveKeypress())},
		{name: "the archived view, switched to with left or right", room: 120, height: 24, cursor: 0, isArchivedView: true},
		{name: "the confirmation asked before an archived session is restored", room: 120, height: 24, cursor: 1, isArchivedView: true, removalKey: new(archiveKeypress())},
		{name: "the archived view with nothing archived", room: 120, height: 24, cursor: 0, isArchivedView: true, hasNothingArchived: true},
		{name: "the confirmation asked before a session is deleted for good", room: 120, height: 24, cursor: 1, removalKey: new(deleteKeypress())},
		{name: "the deletion confirmation clipped by a narrow terminal", room: 46, height: 24, cursor: 1, removalKey: new(deleteKeypress())},
		{name: "deleting an archived session for good", room: 120, height: 24, cursor: 0, isArchivedView: true, removalKey: new(deleteKeypress())},
	}

	var output strings.Builder

	for _, frame := range frames {
		paint := func(rows menu.List, room int, height int, cursor int, query string) string {
			if frame.removalKey == nil {
				return menu.Paint(rows, room, height, cursor, query)
			}

			return menu.PaintRemoval(rows, room, height, cursor, query, *frame.removalKey)
		}

		archived := archivedSessions()
		if frame.hasNothingArchived {
			archived = nil
		}

		fmt.Fprintf(&output, "=== %s ===\n%s\n", frame.name, strutil.VisibleEscapes(
			paint(
				&sessionList{
					store: Store{
						Sessions:         storedSessions(),
						ArchivedSessions: archived,
						Archive:          archiving(),
						Restore:          archiving(),
						Delete:           archiving(),
					},
					isArchivedView: frame.isArchivedView,
				},
				frame.room,
				frame.height,
				frame.cursor,
				frame.query,
			),
		))
	}

	compareWithGolden(t, "painted.ansi", output.String())
}

func archiving() func(*Session) error {
	return func(*Session) error { return nil }
}
