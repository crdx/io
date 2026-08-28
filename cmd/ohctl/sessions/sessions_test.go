package sessions

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/location"
	"crdx.org/io/cmd/oh/sessionPicker"
	"crdx.org/io/cmd/oh/store"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/oh/width"
	"crdx.org/io/session"

	ohSessions "crdx.org/io/cmd/oh/sessions"
)

func TestARunningSessionIsReportedAsRunningAndAnEndedOneAsEnded(t *testing.T) {
	directory := t.TempDir()
	workspaceDir := t.TempDir()

	writer, err := store.Create(directory, store.Meta{WorkspaceDir: workspaceDir})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "begin"}); err != nil {
		t.Fatal(err)
	}

	running := load(t, directory, false)
	if len(running) != 1 || running[0].Status != runningStatus || !running[0].IsRunning {
		t.Fatalf("expected one running session, got %+v", running)
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	ended := load(t, directory, false)
	if len(ended) != 1 || ended[0].Status != endedStatus || ended[0].IsRunning {
		t.Fatalf("expected one ended session, got %+v", ended)
	}
}

func TestOnlyRunningSessionsAreListedWhenAskedFor(t *testing.T) {
	directory := t.TempDir()

	writer, err := store.Create(directory, store.Meta{WorkspaceDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "begin"}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	if listings := load(t, directory, true); len(listings) != 0 {
		t.Errorf("expected the ended session to be left out, got %+v", listings)
	}
}

func TestAListingSaysWhereASessionKeepsItsFiles(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv(location.StateDirVariable, stateDir)

	directory := t.TempDir()
	writer, err := store.Create(directory, store.Meta{WorkspaceDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close() })
	if err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "begin"}); err != nil {
		t.Fatal(err)
	}

	listings := load(t, directory, false)
	if len(listings) != 1 {
		t.Fatalf("expected one session, got %d", len(listings))
	}

	listing := listings[0]
	if listing.SessionDir != session.Dir(directory, listing.Name) {
		t.Errorf("unexpected session directory %q", listing.SessionDir)
	}
	if want := filepath.Join(stateDir, "tmps", listing.Name); listing.ScratchDir != want {
		t.Errorf("got scratch directory %q, want %q", listing.ScratchDir, want)
	}
}

func TestTheTableNamesEveryColumnAndAlignsThem(t *testing.T) {
	var written strings.Builder
	if err := writeTable(sample(), &written); err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimRight(style.Plain(written.String()), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected a header and two rows, got %d lines", len(lines))
	}

	header := lines[0]
	for _, column := range []string{"Status", "Agent", "Title", "Messages", "Length", "Last Message", "Model", "Effort", "Workspace"} {
		if !strings.Contains(header, column) {
			t.Errorf("expected the header to name %q, got %q", column, header)
		}
	}

	at := columnStart(t, header, "Workspace")
	for _, line := range lines[1:] {
		if got := columnStart(t, line, "/workspace"); got != at {
			t.Errorf("expected the workspace column at cell %d, got %d in %q", at, got, line)
		}
	}
	if !strings.HasPrefix(lines[1], runningStatus) || !strings.HasPrefix(lines[2], endedStatus) {
		t.Errorf("expected each row to lead with its status, got %q and %q", lines[1], lines[2])
	}
}

func TestAWideTitleIsElidedAndAnEmojiStillLinesUp(t *testing.T) {
	now := time.Now()
	listings := []Listing{
		{
			Name: "canny-parrot", Status: runningStatus, Title: "fix-blank-title 🟡",
			Model: "gpt-5.6-sol", WorkspaceDir: "/workspace/io", Started: now, Touched: now,
		},
		{
			Name: "wild-scorpion", Status: endedStatus, Title: strings.Repeat("long-", 40),
			Model: "claude-opus-5", WorkspaceDir: "/workspace/elsewhere", Started: now, Touched: now,
		},
	}

	var written strings.Builder
	if err := writeTable(listings, &written); err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimRight(style.Plain(written.String()), "\n"), "\n")
	if !strings.Contains(lines[2], "…") {
		t.Errorf("expected the wide title to be elided, got %q", lines[2])
	}

	at := columnStart(t, lines[0], "Workspace")
	for _, line := range lines[1:] {
		if got := columnStart(t, line, "/workspace"); got != at {
			t.Errorf("expected the workspace column at cell %d, got %d in %q", at, got, line)
		}
	}
}

func TestATitleModelAndEffortAreShownAsADashWhenThereIsNone(t *testing.T) {
	var written strings.Builder
	if err := writeTable([]Listing{{Name: "chewy-raven", Status: endedStatus}}, &written); err != nil {
		t.Fatal(err)
	}

	if strings.Count(written.String(), "—") != 3 {
		t.Errorf("expected a dash for the title, model, and effort, got %q", written.String())
	}
}

func TestTheJSONListingCarriesWhatTheTableCannot(t *testing.T) {
	var written strings.Builder
	if err := writeJSON(sample(), &written); err != nil {
		t.Fatal(err)
	}

	var decoded []Listing
	if err := json.Unmarshal([]byte(written.String()), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded) != 2 {
		t.Fatalf("expected two listings, got %d", len(decoded))
	}
	if !decoded[0].IsRunning || decoded[1].IsRunning {
		t.Errorf("expected the running session to be marked, got %+v", decoded)
	}
	if decoded[0].ScratchDir != "/tmps/wild-scorpion" {
		t.Errorf("expected the scratch directory to be carried, got %q", decoded[0].ScratchDir)
	}
	if decoded[0].Touched.IsZero() {
		t.Error("expected the moment it was last touched to be carried")
	}
}

func TestATitleSpanningLinesIsFlattenedIntoOne(t *testing.T) {
	listings := describe("/sessions", []*sessionPicker.Session{
		{Name: "chewy-raven", Title: "check\nunmerged\nwork"},
	}, false)

	if listings[0].Title != "check unmerged work" {
		t.Errorf("expected the title on one line, got %q", listings[0].Title)
	}
}

func columnStart(t *testing.T, line string, marker string) int {
	t.Helper()

	before, _, found := strings.Cut(line, marker)
	if !found {
		t.Fatalf("expected %q in %q", marker, line)
	}

	return width.Of(before)
}

func load(t *testing.T, directory string, runningOnly bool) []Listing {
	t.Helper()

	stored, err := ohSessions.Load(directory)
	if err != nil {
		t.Fatal(err)
	}

	return describe(directory, stored, runningOnly)
}

func sample() []Listing {
	started := time.Now().Add(-3 * time.Hour)

	return []Listing{
		{
			Name:         "wild-scorpion",
			Status:       runningStatus,
			IsRunning:    true,
			Title:        "audit-golden-files",
			WorkspaceDir: "/workspace/io",
			ScratchDir:   "/tmps/wild-scorpion",
			SessionDir:   "/sessions/wild-scorpion",
			Model:        "claude-opus-5",
			Effort:       "medium",
			Messages:     20,
			Started:      started,
			Touched:      time.Now(),
		},
		{
			Name:         "dewy-vole",
			Status:       endedStatus,
			Title:        "retry-payload",
			WorkspaceDir: "/workspace/io",
			ScratchDir:   "/tmps/dewy-vole",
			SessionDir:   "/sessions/dewy-vole",
			Model:        "gpt-5.3-codex",
			Effort:       "high",
			Messages:     8,
			Started:      started,
			Touched:      started.Add(time.Hour),
		},
	}
}
