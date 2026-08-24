package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"iter"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/dynamic"
	"crdx.org/io/cmd/oh/edit"
	"crdx.org/io/cmd/oh/input"
	"crdx.org/io/cmd/oh/output"
	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/startup"
	"crdx.org/io/cmd/oh/store/transcript"
	"crdx.org/io/internal/file"
	"crdx.org/io/toolbox"
)

const (
	replayColumns = 100
	replayLines   = 24
	narrowColumns = 40
	tinyColumns   = 12
	oneColumn     = 1
	noColumns     = 0

	turnSoFar = 9 * time.Second

	workspaceMarker   = "/workspace"
	lifecycleScenario = "success@rxw.jsonl"
)

var updateGoldens = flag.Bool("update", false, "write what was drawn back to the golden files")

type replayEntry struct {
	Event *agent.Event `json:"event,omitempty"`
}

func TestEveryScenarioShowsWhatItShowedBefore(t *testing.T) {
	for _, journal := range everyJournal(t) {
		t.Run(journal.name, func(t *testing.T) {
			entries := readJournal(t, journal.path)

			compareWithGolden(t, journal.name, ".screen", map[string]func() string{
				"wide":       func() string { return shownAtWidth(t, entries, replayColumns) },
				"narrow":     func() string { return shownAtWidth(t, entries, narrowColumns) },
				"tiny":       func() string { return shownAtWidth(t, entries, tinyColumns) },
				"one column": func() string { return shownAtWidth(t, entries, oneColumn) },
			})
		})
	}
}

func shownAtWidth(t *testing.T, entries []replayEntry, columns int) string {
	t.Helper()

	return shown(t, replayAtWidth(t, entries, columns), columns)
}

func shown(t *testing.T, stream string, columns int) string {
	t.Helper()

	return strings.Join(visibleScreen(t, stream, columns), "\n")
}

func shownPasses(t *testing.T, passes map[string]func() string) map[string]func() string {
	t.Helper()

	shownAt := map[string]func() string{}

	for name, pass := range passes {
		shownAt[name] = func() string { return shown(t, pass(), replayColumns) }
	}

	return shownAt
}

func TestEveryScenarioDrawsWhatItDrewBefore(t *testing.T) {
	for _, journal := range everyJournal(t) {
		t.Run(journal.name, func(t *testing.T) {
			entries := readJournal(t, journal.path)

			compareWithGolden(t, journal.name, ".ansi", map[string]func() string{
				"wide":       func() string { return replayAtWidth(t, entries, replayColumns) },
				"narrow":     func() string { return replayAtWidth(t, entries, narrowColumns) },
				"tiny":       func() string { return replayAtWidth(t, entries, tinyColumns) },
				"unsized":    func() string { return replayAtWidth(t, entries, noColumns) },
				"one column": func() string { return replayAtWidth(t, entries, oneColumn) },
				"streamed":   func() string { return streamIntoBuffer(t, entries) },
				"plain":      func() string { return replayPlainly(t, entries) },
			})
		})
	}
}

func TestEverySessionIsWrittenDownTheSameWay(t *testing.T) {
	for _, journal := range everyJournal(t) {
		t.Run(journal.name, func(t *testing.T) {
			entries := readJournal(t, journal.path)

			compareWithGolden(t, journal.name, ".transcript", map[string]func() string{
				"written down": func() string { return writeTranscript(t, entries) },
			})
		})
	}
}

func writeTranscript(t *testing.T, entries []replayEntry) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "chat.md")

	recorder, err := transcript.Open(path, transcript.Meta{
		Name:      "brave-otter",
		Model:     "gpt-5.6-sol",
		Effort:    "high",
		Provider:  "codex",
		Workspace: workspaceMarker,
		Started:   transcriptTime,
	})
	if err != nil {
		t.Fatal(err)
	}

	for at, entry := range entries {
		if entry.Event == nil {
			continue
		}

		when := transcriptTime.Add(time.Duration(at) * time.Second)
		if err := recorder.Event(when, *entry.Event); err != nil {
			t.Fatal(err)
		}
	}

	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}

	//nolint:gosec // the path names a file this test just wrote in its own temporary directory
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	return string(written)
}

var transcriptTime = time.Date(2001, time.January, 1, 0, 0, 0, 0, time.UTC)

func TestTheScreenAroundAConversationDrawsWhatItDrewBefore(t *testing.T) {
	entries := readJournal(t, filepath.Join("testdata", "input", lifecycleScenario))

	passes := map[string]func() string{}

	for _, screen := range []struct {
		name string
		open func(*testing.T) *replayRig
	}{
		{name: "", open: newWideRig},
		{name: " on a pipe", open: newPlainRig},
	} {
		passes["under a footer"+screen.name] = func() string {
			return replayUnderFooter(t, screen.open, entries)
		}
		passes["resized"+screen.name] = func() string {
			return replayThenRedraw(t, screen.open, entries, false)
		}
		passes["resized mid-turn"+screen.name] = func() string {
			return replayThenRedraw(t, screen.open, entries, true)
		}
		passes["released and kept"+screen.name] = func() string {
			return replayThenRelease(t, screen.open, entries, true)
		}
		passes["released and unused"+screen.name] = func() string {
			return replayThenRelease(t, screen.open, entries, false)
		}
	}

	compareWithGolden(t, "lifecycle", ".ansi", passes)
	compareWithGolden(t, "lifecycle", ".screen", shownPasses(t, onATerminal(passes)))
}

func onATerminal(passes map[string]func() string) map[string]func() string {
	kept := map[string]func() string{}

	for name, pass := range passes {
		if !strings.HasSuffix(name, " on a pipe") {
			kept[name] = pass
		}
	}

	return kept
}

func TestATurnStillRunningDrawsWhatItDrewBefore(t *testing.T) {
	entries := readJournal(t, filepath.Join("testdata", "input", lifecycleScenario))

	passes := map[string]func() string{
		"a call still running": func() string { return replayWhileRunning(t, entries) },
		"discarded reasoning":  func() string { return drawDiscardedReasoning(t) },
	}

	compareWithGolden(t, "running", ".ansi", passes)
	compareWithGolden(t, "running", ".screen", shownPasses(t, passes))
}

func drawDiscardedReasoning(t *testing.T) string {
	t.Helper()

	rig := newReplayRig(t, replayColumns)
	rig.chat.currentTurn = Turn{isRunning: true, painter: rig.chat.newPainter(true)}

	completeThought := agent.Event{Kind: agent.ModelReasoningEvent, Text: "complete thought"}
	rig.chat.takeTurn(TurnEvent{update: agent.Update{Event: &completeThought}})
	incompleteThought := agent.Delta{Kind: agent.ModelReasoningEvent, Text: "incomplete thought"}
	rig.chat.takeTurn(TurnEvent{update: agent.Update{Delta: &incompleteThought}})
	failure := agent.Event{Kind: agent.FailureEvent, Text: "stream failed"}
	rig.chat.takeTurn(TurnEvent{update: agent.Update{Event: &failure}})
	rig.chat.screen.End()

	return strings.TrimSuffix(rig.drawn(), "\r\n")
}

type journal struct {
	name string
	path string
}

func everyJournal(t *testing.T) []journal {
	t.Helper()

	paths, err := filepath.Glob(filepath.Join("testdata", "input", "*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}

	if len(paths) == 0 {
		t.Fatal("expected the journals to be found")
	}

	journals := make([]journal, 0, len(paths))

	for _, path := range paths {
		name := strings.TrimSuffix(filepath.Base(path), ".jsonl")
		journals = append(journals, journal{name: name, path: path})
	}

	return journals
}

func drawnOnAStoppedClock(t *testing.T, draw func(t *testing.T) string) string {
	t.Helper()

	var drawn string

	synctest.Test(t, func(t *testing.T) { drawn = draw(t) })

	return drawn
}

func compareWithGolden(t *testing.T, name string, suffix string, passes map[string]func() string) {
	t.Helper()

	var drawn strings.Builder

	for _, pass := range slices.Sorted(maps.Keys(passes)) {
		fmt.Fprintf(&drawn, "=== %s ===\n%s\n", pass, visibleEscapes(passes[pass]()))
	}

	goldenPath := filepath.Join("testdata", "output", name+suffix)

	if *updateGoldens {
		if err := os.WriteFile(goldenPath, []byte(drawn.String()), 0o600); err != nil {
			t.Fatal(err)
		}

		return
	}

	//nolint:gosec // the path names a golden beside the journal it belongs to
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("%v: write the goldens with `just -f rig.just goldens`", err)
	}

	if drawn.String() != string(want) {
		t.Errorf(
			"%s drew something else; write the goldens again and read the diff\n"+
				"--- drawn ---\n%s\n--- golden ---\n%s",
			name, drawn.String(), want,
		)
	}
}

func readJournal(t *testing.T, path string) []replayEntry {
	t.Helper()

	//nolint:gosec // the path names a journal in testdata
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var entries []replayEntry

	for number, line := range strings.Split(strings.TrimSpace(string(contents)), "\n") {
		var entry replayEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("%s: line %d: %v", path, number+1, err)
		}

		entries = append(entries, entry)
	}

	return entries
}

type replayRig struct {
	chat         *Harness
	written      *strings.Builder
	workspaceDir string
}

func newReplayRig(t *testing.T, columns int) *replayRig {
	t.Helper()

	return newRig(t, func(written *strings.Builder, workspaceDir string) *output.Screen {
		return output.NewTerminalOfSize(written, columns, replayLines).LinkPathsUnder(workspaceDir)
	})
}

func newWideRig(t *testing.T) *replayRig {
	t.Helper()

	return newReplayRig(t, replayColumns)
}

func newPlainRig(t *testing.T) *replayRig {
	t.Helper()

	return newRig(t, func(written *strings.Builder, workspaceDir string) *output.Screen {
		return output.New(written).LinkPathsUnder(workspaceDir)
	})
}

func newRig(t *testing.T, openScreen func(*strings.Builder, string) *output.Screen) *replayRig {
	t.Helper()

	workspaceDir := layOutWorkspace(t)

	files := file.New(openWorkspaceRootAt(t, workspaceDir), caps.RefuseWrite(caps.NewMode(caps.All())))

	var written strings.Builder

	screen := openScreen(&written, workspaceDir)

	return &replayRig{
		written:      &written,
		workspaceDir: workspaceDir,
		chat: &Harness{
			agent:        agent.New("", quietProvider{}, toolbox.Rummage(files, file.NewSnapshots())),
			screen:       screen,
			workspaceDir: workspaceDir,
			recorder:     recordSession(testLog(t)),
		},
	}
}

func (self *replayRig) load(entries []replayEntry) {
	for _, entry := range entries {
		self.chat.events = append(self.chat.events, *entry.Event)
	}
}

func (self *replayRig) drawn() string {
	return strings.ReplaceAll(self.written.String(), self.workspaceDir, workspaceMarker)
}

func replayAtWidth(t *testing.T, entries []replayEntry, columns int) string {
	t.Helper()

	return replayInto(newReplayRig(t, columns), entries)
}

func replayPlainly(t *testing.T, entries []replayEntry) string {
	t.Helper()

	return replayInto(newPlainRig(t), entries)
}

func replayInto(rig *replayRig, entries []replayEntry) string {
	rig.load(entries)
	rig.chat.replay()

	return rig.drawn()
}

func streamIntoBuffer(t *testing.T, entries []replayEntry) string {
	t.Helper()

	rig := newReplayRig(t, replayColumns)
	rig.chat.currentTurn = Turn{isRunning: true, painter: rig.chat.newPainter(true)}
	rig.chat.screen.ReportProgress(true)

	for _, entry := range entries {
		event := *entry.Event
		if event.Kind == agent.ModelMessageEvent || event.Kind == agent.ModelReasoningEvent {
			for piece := range deltaSized(event.Text) {
				rig.chat.currentTurn.painter.drawDelta(agent.Delta{Kind: event.Kind, Text: piece})
			}
		}

		rig.chat.events = append(rig.chat.events, event)
		rig.chat.currentTurn.painter.drawEvent(event)

		if rig.chat.currentTurn.painter.isStale {
			rig.chat.redraw()
		}
	}

	rig.chat.currentTurn.painter.close(dynamic.Done)
	rig.chat.screen.End()
	rig.chat.screen.ReportProgress(false)

	return rig.drawn()
}

func deltaSized(text string) iter.Seq[string] {
	return func(yield func(string) bool) {
		runes := []rune(text)

		for at := 0; at < len(runes); at += deltaRunes {
			if !yield(string(runes[at:min(at+deltaRunes, len(runes))])) {
				return
			}
		}
	}
}

const deltaRunes = 4

func replayUnderFooter(t *testing.T, openRig func(*testing.T) *replayRig, entries []replayEntry) string {
	t.Helper()

	rig := openRig(t)

	for range 2 {
		rig.chat.screen.Footer([]string{footerPrompt, "> and a second row"}, 1, 0)
	}

	rig.load(entries)
	rig.chat.replay()

	return rig.drawn()
}

const footerPrompt = "> ask something else"

func replayThenRedraw(t *testing.T, openRig func(*testing.T) *replayRig, entries []replayEntry, isRunning bool) string {
	t.Helper()

	rig := openRig(t)
	rig.chat.currentTurn.isRunning = isRunning
	rig.load(entriesWhile(entries, isRunning))
	rig.chat.replay()
	rig.chat.redraw()

	if isRunning {
		rig.chat.currentTurn.painter.close(dynamic.Cancelled)
	}

	return rig.drawn()
}

func entriesWhile(entries []replayEntry, isRunning bool) []replayEntry {
	if !isRunning {
		return entries
	}

	return entriesUpToFirstCall(entries)
}

func replayThenRelease(t *testing.T, openRig func(*testing.T) *replayRig, entries []replayEntry, shouldKeep bool) string {
	t.Helper()

	rig := openRig(t)
	rig.chat.screen.ReportProgress(true)
	rig.chat.screen.ReportProgress(true)
	rig.load(entries)
	rig.chat.replay()
	rig.chat.screen.Footer([]string{footerPrompt}, 0, len(footerPrompt))
	rig.chat.screen.Release(shouldKeep)

	return rig.drawn()
}

func replayWhileRunning(t *testing.T, entries []replayEntry) string {
	t.Helper()

	var drawn string

	synctest.Test(t, func(t *testing.T) {
		rig := newReplayRig(t, replayColumns)
		rig.chat.currentTurn.isRunning = true
		rig.load(entriesUpToFirstCall(entries))
		rig.chat.replay()

		time.Sleep(revealAndSomeFrames)
		synctest.Wait()

		drawn = rig.drawn()

		rig.chat.currentTurn.painter.close(dynamic.Cancelled)
	})

	return drawn
}

const revealAndSomeFrames = 7 * time.Second

func entriesUpToFirstCall(entries []replayEntry) []replayEntry {
	for at, entry := range entries {
		if entry.Event != nil && entry.Event.Kind == agent.ToolCallRequestEvent {
			return entries[:at+1]
		}
	}

	return entries
}

func openWorkspaceRootAt(t *testing.T, workspaceDir string) *os.Root {
	t.Helper()

	workspaceRoot, err := os.OpenRoot(workspaceDir)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = workspaceRoot.Close() })

	return workspaceRoot
}

func layOutWorkspace(t *testing.T) string {
	t.Helper()

	workspaceDir := filepath.Join(t.TempDir(), "workspace")

	t.Setenv("HOME", workspaceDir)

	if err := os.CopyFS(workspaceDir, os.DirFS(filepath.Join("testdata", "input", "workspace"))); err != nil {
		t.Fatal(err)
	}

	return workspaceDir
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

func TestALiveTurnLeavesTheSameScreenAsAReplayOfIt(t *testing.T) {
	for _, journal := range everyJournal(t) {
		t.Run(journal.name, func(t *testing.T) {
			entries := readJournal(t, journal.path)

			replayed := visibleScreen(t, replayAtWidth(t, entries, replayColumns), replayColumns)
			live := visibleScreen(t, streamIntoBuffer(t, entries), replayColumns)

			if !slices.Equal(replayed, live) {
				t.Errorf(
					"a live turn and a replay of it left different screens\n--- replayed ---\n%s\n--- live ---\n%s",
					strings.Join(replayed, "\n"), strings.Join(live, "\n"),
				)
			}
		})
	}
}

func TestTheBannerDrawsWhatItDrewBefore(t *testing.T) {
	passes := map[string]func() string{}

	for _, flags := range []string{"", "r", "rw", "rx", "rxw", "rxwg", "rxwgb", "rgb"} {
		grantedCaps, err := caps.Parse(flags)
		if err != nil {
			t.Fatal(err)
		}

		for _, isRunning := range []bool{false, true} {
			name := "caps " + flags + " while waiting"
			if isRunning {
				name = "caps " + flags + " while running"
			}

			passes[name] = func() string {
				return drawnOnAStoppedClock(t, func(t *testing.T) string {
					t.Helper()

					held := &Harness{mode: caps.NewMode(grantedCaps)}
					held.currentTurn.isRunning = isRunning
					held.currentTurn.spinnerFrame = 2
					held.currentTurn.startedAt = time.Now().Add(-turnSoFar)

					built := goldenBarLayout(t, held)

					return bar(built, segment.BottomLeft, edit.Frame{})
				})
			}
		}
	}

	for name, inputTokens := range map[string]int{
		"context usage low":  5000,
		"context usage high": 182_000,
	} {
		passes[name] = func() string {
			held := &Harness{
				mode:                caps.NewMode(caps.All()),
				contextWindowTokens: 200_000,
				events: []agent.Event{{
					Kind:  agent.ModelMessageEvent,
					Usage: &agent.Usage{InputTokens: inputTokens},
				}},
			}

			built := goldenBarLayout(t, held)

			return bar(built, segment.BottomLeft, edit.Frame{})
		}
	}

	passes["the startup line"] = func() string {
		return startup.RenderBanner(1500*time.Microsecond, false, startup.Info{
			Session: "brave-otter",
			ContextFiles: []startup.File{
				{Name: "SYSTEM.md", Bytes: 740},
				{Name: "AGENTS.md", Bytes: 3 * 1024},
			},
			ProjectSkills: 3,
			GlobalSkills:  1,
			ToolBytes:     614,
		})
	}

	compareWithGolden(t, "banner", ".ansi", passes)
	compareWithGolden(t, "banner", ".screen", shownPasses(t, passes))
}

func TestTheInputBlockDrawsWhatItDrewBefore(t *testing.T) {
	frames := map[string]edit.Frame{
		"one row": {
			Rows: []string{"> what is the weather"}, Row: 0, Column: 21,
		},
		"scrolled both ways": {
			Rows: []string{"> the third row", "> the fourth row"}, Row: 1, Column: 16,
			HiddenLinesAbove: 2, HiddenLinesBelow: 7,
		},
	}

	passes := map[string]func() string{}
	shownPassesAtWidth := map[string]func() string{}

	for _, width := range []int{80, 40, 20} {
		for name, frame := range frames {
			passName := fmt.Sprintf("%s at %d columns", name, width)

			passes[passName] = func() string {
				return drawnOnAStoppedClock(t, func(t *testing.T) string {
					t.Helper()

					held := &Harness{mode: caps.NewMode(caps.All())}
					held.currentTurn.isRunning = true
					held.currentTurn.spinnerFrame = 2
					held.currentTurn.startedAt = time.Now().Add(-turnSoFar)

					built := goldenBarLayout(t, held)

					held.segmentLayout = built

					block := input.Block{
						Top: input.Ruler{
							Left:   held.bar(segment.TopLeft, frame),
							Center: held.bar(segment.TopCenter, frame),
							Right:  held.bar(segment.TopRight, frame),
						},
						Input: frame,
						Bottom: input.Ruler{
							Left:   held.bar(segment.BottomLeft, frame),
							Center: held.bar(segment.BottomCenter, frame),
							Right:  held.bar(segment.BottomRight, frame),
						},
					}

					rows, cursorRow, cursorColumn := block.Rows(width)

					return fmt.Sprintf(
						"%s\ncursor row %d column %d",
						strings.Join(rows, "\n"), cursorRow, cursorColumn,
					)
				})
			}

			shownPassesAtWidth[passName] = func() string {
				drawn := passes[passName]()

				return shown(t, drawn[:strings.LastIndex(drawn, "\ncursor row ")], width)
			}
		}
	}

	compareWithGolden(t, "inputblock", ".ansi", passes)
	compareWithGolden(t, "inputblock", ".screen", shownPassesAtWidth)
}
