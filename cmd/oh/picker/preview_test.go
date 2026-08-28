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

func storedSessions() []*Session {
	now := time.Now()

	return []*Session{
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
		t.Errorf("picker differs from %s\n--- got ---\n%s--- want ---\n%s", goldenPath, drawn, want)
	}
}

func TestPickerMatchesTheGolden(t *testing.T) {
	sessions := storedSessions()

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

	compareWithGolden(t, "picker.golden", output.String())
}

func TestWhatThePickerPaintsMatchesTheGolden(t *testing.T) {
	frames := []struct {
		name    string
		room    int
		height  int
		cursor  int
		measure func() (int, int)
	}{
		{name: "the cursor resting on the first session that can be chosen", room: 80, height: 24, cursor: 1},
		{name: "the cursor further down the list", room: 80, height: 24, cursor: 3},
		{name: "no room for every row, so the list is scrolled to the cursor", room: 80, height: 6, cursor: 4},
		{name: "a narrow terminal, where the columns are clipped", room: 46, height: 24, cursor: 1},
		{name: "one row of room, which is as small as the list goes", room: 80, height: 1, cursor: 2},
	}

	var output strings.Builder

	for _, frame := range frames {
		var screen strings.Builder

		self := &state{
			sessions:     storedSessions(),
			workspaceDir: "/workspace",
			cursor:       frame.cursor,
			screen:       &screen,
			measure:      func() (int, int) { return frame.room, frame.height },
		}

		self.draw()

		fmt.Fprintf(&output, "=== %s ===\n%s\n", frame.name, visibleEscapes(screen.String()))
	}

	compareWithGolden(t, "painted.ansi", output.String())
}

func visibleEscapes(stream string) string {
	var out strings.Builder

	for _, character := range stream {
		switch {
		case character == '\n':
			out.WriteByte('\n')
		case character == '\\':
			out.WriteString(`\\`)
		case character == '\x1b':
			out.WriteString(`\e`)
		case character == '\r':
			out.WriteString(`\r`)
		case character == '\t':
			out.WriteString(`\t`)
		case character < ' ' || character == 0x7f:
			fmt.Fprintf(&out, `\x%02X`, character)
		default:
			out.WriteRune(character)
		}
	}

	return out.String()
}
