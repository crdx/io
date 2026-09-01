package menu

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"crdx.org/io/cmd/oh/key"
	"crdx.org/io/internal/util/strutil"
)

var updateGoldens = flag.Bool("update", false, "write what was drawn back to the golden files")

func paintedRows() *fakeList {
	return &fakeList{
		rows: []string{
			"chewy-sardine   why does the spinner stutter when a tool runs",
			"thick-poodle    add support for reasoning traces",
			"funny-badger    the cancelled turn leaves a tool call unanswered",
			"able-dolphin    (untitled)",
			"brave-otter     rename the harness to oh",
		},
		unrunnable: []bool{true, false, false, false, false},
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
		t.Errorf("menu differs from %s\n--- got ---\n%s--- want ---\n%s", goldenPath, drawn, want)
	}
}

func TestWhatAMenuPaintsMatchesTheGolden(t *testing.T) {
	frames := []struct {
		name   string
		room   int
		height int
		cursor int
		query  string
	}{
		{name: "the cursor resting on the first row that can be chosen", room: 80, height: 24, cursor: 1},
		{name: "the cursor further down the list", room: 80, height: 24, cursor: 3},
		{name: "no room for every row, so the list is scrolled to the cursor", room: 80, height: 6, cursor: 4},
		{name: "a narrow terminal, where the columns are clipped", room: 46, height: 24, cursor: 1},
		{name: "one row of room, which is as small as the list goes", room: 80, height: 1, cursor: 2},
		{name: "a filter narrowing the list to what was typed", room: 80, height: 24, cursor: 1, query: "the"},
		{name: "a filter nothing answers to", room: 80, height: 24, cursor: -1, query: "nothing at all"},
	}

	var output strings.Builder

	for _, frame := range frames {
		fmt.Fprintf(&output, "=== %s ===\n%s\n", frame.name, strutil.VisibleEscapes(
			Paint(paintedRows(), frame.room, frame.height, frame.cursor, frame.query),
		))
	}

	compareWithGolden(t, "painted.ansi", output.String())
}

func TestTheCompleteMenuLifecycleMatchesTheGolden(t *testing.T) {
	keys := make(chan key.Key, 3)
	keys <- key.Key{Code: key.Down}
	keys <- key.Key{Code: key.Rune, Value: 'f'}
	keys <- key.Key{Code: key.Enter}
	close(keys)

	var output strings.Builder
	chosen, err := choose(
		paintedRows(),
		keys,
		func() (int, int) { return 46, 6 },
		&output,
	)
	if err != nil {
		t.Fatal(err)
	}
	if chosen != 2 {
		t.Errorf("chose row %d, want 2", chosen)
	}

	compareWithGolden(t, "lifecycle.ansi", strutil.VisibleEscapes(output.String()))
}

func TestTheCompleteRemovalLifecycleMatchesTheGolden(t *testing.T) {
	rows := &removableList{
		fakeList: fakeList{
			rows: []string{
				"chewy-sardine   why does the spinner stutter when a tool runs",
				"thick-poodle    add support for reasoning traces",
				"funny-badger    the cancelled turn leaves a tool call unanswered",
				"able-dolphin    (untitled)",
			},
			unrunnable: []bool{true, false, false, false},
		},
		refused: []string{"able-dolphin    (untitled)"},
	}

	keypresses := []key.Key{
		{Code: key.Delete},
		{Code: key.Rune, Value: 'n'},
		{Code: key.Delete},
		{Code: key.Rune, Value: 'y'},
		{Code: key.End},
		{Code: key.Delete},
		{Code: key.Rune, Value: 'y'},
		{Code: key.Up},
		{Code: key.Enter},
	}

	keys := make(chan key.Key, len(keypresses))
	for _, keypress := range keypresses {
		keys <- keypress
	}
	close(keys)

	var output strings.Builder
	chosen, err := choose(rows, keys, func() (int, int) { return 46, 6 }, &output)
	if err != nil {
		t.Fatal(err)
	}
	if chosen != 1 {
		t.Errorf("chose row %d, want 1", chosen)
	}
	if !slices.Equal(rows.removed, []string{"thick-poodle    add support for reasoning traces"}) {
		t.Errorf("got the rows removed as %v", rows.removed)
	}

	compareWithGolden(t, "removal.ansi", strutil.VisibleEscapes(output.String()))
}
