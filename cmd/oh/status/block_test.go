package status

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"crdx.org/io/cmd/oh/markdown"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/internal/util"
	"crdx.org/io/tool"
)

const wide = 100

func TestStatsAreShownAfterCalls(t *testing.T) {
	for name, test := range map[string]struct {
		stats tool.Stats
		want  []string
	}{
		"output": {
			stats: tool.Stats{Kind: tool.StatsOutput, Lines: 4, Bytes: 1200, TotalBytes: 1200},
			want:  []string{"4L ~300t"},
		},
		"empty output": {
			stats: tool.Stats{Kind: tool.StatsOutput},
			want:  []string{"no output"},
		},
		"capped output": {
			stats: tool.Stats{
				Kind: tool.StatsOutput, Lines: 4, Bytes: 1200, TotalBytes: 1200, Truncated: true,
			},
			want: []string{"4L+ ~300t"},
		},
		"resources": {
			stats: tool.Stats{
				Kind: tool.StatsResources, CPUTime: 800 * time.Millisecond, PeakMemory: 92 << 20, Lines: 7, Bytes: 1200,
			},
			want: []string{"7L ~300t 1.4s 0.8s 92M"},
		},
		"read": {
			stats: tool.Stats{Kind: tool.StatsRead, Lines: 42, Bytes: 1200},
			want:  []string{"42L ~300t"},
		},
		"list": {
			stats: tool.Stats{Kind: tool.StatsList, Lines: 42},
			want:  []string{"42L"},
		},
		"image": {
			stats: tool.Stats{Kind: tool.StatsImage, Bytes: 80_943, EstimatedTokens: 1536},
			want:  []string{"~1.5Kt"},
		},
		"write": {
			stats: tool.Stats{Kind: tool.StatsWrite, Lines: 3, Bytes: 17},
			want:  []string{"3L ~5t"},
		},
		"diff": {
			stats: tool.Stats{Kind: tool.StatsDiff, Added: 3, Removed: 2},
			want:  []string{"+3 −2"},
		},
		"search": {
			stats: tool.Stats{Kind: tool.StatsSearch, Lines: 17, Bytes: 1200},
			want:  []string{"17L ~300t"},
		},
		"capped search": {
			stats: tool.Stats{
				Kind: tool.StatsSearch, Lines: 100, Bytes: 32_000, TotalBytes: 80_000, Truncated: true,
			},
			want: []string{"100L+ ~8Kt (of ~20Kt)"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			got := style.Plain(outcomeText("✓", 1400*time.Millisecond, &test.stats))
			for _, want := range test.want {
				if !strings.Contains(got, want) {
					t.Errorf("got %q, want %q", got, want)
				}
			}
		})
	}
}

func TestStatsUseTheirExpectedStyles(t *testing.T) {
	output := outcomeText("✓", 0, &tool.Stats{Kind: tool.StatsOutput, Lines: 4, Bytes: 951})
	if want := style.Subtle("4L ~200t"); !strings.Contains(output, want) {
		t.Errorf("output stats got %q, want styled %q", output, want)
	}

	emptyOutput := outcomeText("✓", 0, &tool.Stats{Kind: tool.StatsOutput})
	if want := style.Subtle("no output"); !strings.Contains(emptyOutput, want) {
		t.Errorf("empty output stats got %q, want styled %q", emptyOutput, want)
	}

	read := outcomeText("✓", 0, &tool.Stats{Kind: tool.StatsRead, Lines: 45, Bytes: 951})
	if want := style.Subtle("45L ~200t"); !strings.Contains(read, want) {
		t.Errorf("read stats got %q, want styled %q", read, want)
	}

	write := outcomeText("✓", 0, &tool.Stats{Kind: tool.StatsWrite, Lines: 12, Bytes: 1200})
	if want := style.Subtle("12L ~300t"); !strings.Contains(write, want) {
		t.Errorf("write stats got %q, want styled %q", write, want)
	}

	search := outcomeText("✓", 0, &tool.Stats{
		Kind: tool.StatsSearch, Lines: 23, Bytes: 1200, TotalBytes: 2400, Truncated: true,
	})
	if want := style.Subtle("23L+ ~300t (of ~600t)"); !strings.Contains(search, want) {
		t.Errorf("search stats got %q, want styled %q", search, want)
	}

	exec := outcomeText("✓", 0, &tool.Stats{
		Kind: tool.StatsResources, PeakMemory: 26 << 20,
	})
	wantExec := style.Subtle("0L 0t 0s 0s 26M")
	if !strings.Contains(exec, wantExec) {
		t.Errorf("exec stats got %q, want styled %q", exec, wantExec)
	}

	edit := outcomeText("✓", 0, &tool.Stats{Kind: tool.StatsDiff, Added: 2, Removed: 1})
	wantEdit := style.Success("+2") + style.Subtle(" ") + style.Failure("−1")
	if !strings.Contains(edit, wantEdit) {
		t.Errorf("edit stats got %q, want styled %q", edit, wantEdit)
	}
}

func testBlock() (*Block, *strings.Builder) {
	output := &strings.Builder{}

	self := &Block{
		print:   func(text string) { output.WriteString(text) },
		overlay: func(text string, _ int) { output.WriteString(text) },
		live:    true,
		columns: wide,
		stop:    make(chan struct{}),
	}

	return self, output
}

func callLabel(name string, subject string) Label {
	return Label{Name: name, Subject: subject, ReadOnly: true}
}

var rise = regexp.MustCompile(`^\x1b\[\d+A`)

func rowsFromOutput(output *strings.Builder) []string {
	text := rise.ReplaceAllString(output.String(), "")

	return strings.Split(strings.ReplaceAll(text, "\r\x1b[K", ""), "\n")
}

func TestOnlyTheFocusedPartOfArgumentsIsPainted(t *testing.T) {
	label := Label{
		Name: "read", Subject: "cmd/oh/draw.go", ReadOnly: true,
		Highlight: tool.Highlight{Kind: tool.HighlightFocus, Value: "draw.go"},
	}
	want := style.Call("read") + " " + style.Subtle("cmd/oh/") + style.Subject("draw.go")

	if got := label.render(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAnAccentAndTheFocusedPartOfArgumentsArePainted(t *testing.T) {
	label := Label{
		Name:        "skill",
		NameStyle:   style.Skill,
		Subject:     "/skills/guard-basics/SKILL.md",
		Highlight:   tool.Highlight{Kind: tool.HighlightFocus, Value: "SKILL.md"},
		Accent:      "guard-basics",
		AccentStyle: style.Skill,
	}
	want := style.Skill("skill") + " " +
		style.Subtle("/skills/") + style.Skill("guard-basics") +
		style.Subtle("/") + style.Subject("SKILL.md")

	if got := label.render(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestArgumentsWithSyntaxAreHighlighted(t *testing.T) {
	label := Label{
		Name: "bash", Subject: "echo one && true",
		Highlight: tool.Highlight{Kind: tool.HighlightSyntax, Value: "bash"},
	}
	want := style.Change("bash") + " " + markdown.Highlight(label.Subject, "bash")

	if got := label.render(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestElidedBashKeepsHighlightingFromTheCompleteCommand(t *testing.T) {
	source := "go list ordinary"
	for name, test := range map[string]struct {
		argumentRoom int
		plain        string
		want         string
	}{
		"inside executable": {
			2,
			"g…",
			style.Function("g") + style.Function(ellipsis),
		},
		"at parameter start": {
			4,
			"go …",
			style.Function("go") + style.Block(" ") + style.Function(ellipsis),
		},
		"inside parameter": {
			6,
			"go li…",
			style.Function("go") + style.Block(" ") + style.Function("li") + style.Function(ellipsis),
		},
		"at parameter end": {
			8,
			"go list…",
			style.Function("go") + style.Block(" ") + style.Function("list") + style.Block(ellipsis),
		},
		"inside later argument": {
			12,
			"go list ord…",
			style.Function("go") + style.Block(" ") + style.Function("list") + style.Block(" ord") + style.Block(ellipsis),
		},
	} {
		label := Label{
			Name: "bash", Subject: source,
			Highlight: tool.Highlight{Kind: tool.HighlightSyntax, Value: "bash"},
		}.Elide(len("bash ") + test.argumentRoom)
		got := label.renderSubject()

		if got != test.want {
			t.Errorf("%s: got %q, want %q", name, got, test.want)
		}
		if plain := style.Plain(got); plain != test.plain {
			t.Errorf("%s: got plain text %q, want %q", name, plain, test.plain)
		}
		if cells := style.Width(got); cells > test.argumentRoom {
			t.Errorf("%s: used %d cells, want at most %d", name, cells, test.argumentRoom)
		}
	}
}

func TestElidedBashCountsWideUnicodeInTerminalCells(t *testing.T) {
	argumentRoom := 8
	label := Label{
		Name: "bash", Subject: "echo 日本語 later",
		Highlight: tool.Highlight{Kind: tool.HighlightSyntax, Value: "bash"},
	}.Elide(len("bash ") + argumentRoom)
	got := label.renderSubject()
	want := style.Function("echo") + style.Block(" ") + style.Function("日") + style.Function(ellipsis)

	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if plain := style.Plain(got); plain != "echo 日…" {
		t.Errorf("got plain text %q, want %q", plain, "echo 日…")
	}
	if cells := style.Width(got); cells > argumentRoom {
		t.Errorf("used %d cells, want at most %d", cells, argumentRoom)
	}
}

func TestAPathInTheDetailCanBeFocused(t *testing.T) {
	label := Label{
		Name: "grep", Subject: "text", Qualifier: "in cmd/oh/draw.go",
		Highlight: tool.Highlight{Kind: tool.HighlightFocus, Value: "draw.go"},
	}
	want := style.Change("grep") + " " + style.Subject("text") + " " +
		style.Qualifier("in cmd/oh/") + style.Subject("draw.go")

	if got := label.render(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestARowSaysNothingOfACallUntilItHasBeenGoingAWhile(t *testing.T) {
	block, output := testBlock()

	block.Add(callLabel("read", "main.go"))

	if got := output.String(); got != style.Call("read")+" "+style.Subject("main.go") {
		t.Errorf("expected the call and nothing else, got %q", got)
	}
}

func TestARowIsColouredByWhetherItsCallWrites(t *testing.T) {
	block, output := testBlock()

	block.Add(Label{Name: "write", Subject: "main.go"})

	if got := output.String(); got != style.Change("write")+" "+style.Subject("main.go") {
		t.Errorf("expected a call that writes to be painted as one, got %q", got)
	}
}

func TestAQuickCallIsMarkedWithoutATime(t *testing.T) {
	block, output := testBlock()

	block.Add(callLabel("read", "main.go"))

	output.Reset()
	block.Mark(0, Done, 500*time.Millisecond, "")

	if got := rowsFromOutput(output)[0]; !strings.Contains(got, "✓") || strings.Contains(got, "s") {
		t.Errorf("expected a mark and no time, got %q", got)
	}
}

func TestACallWorthWaitingForSaysHowLongItTook(t *testing.T) {
	block, output := testBlock()

	block.Add(callLabel("read", "main.go"))

	output.Reset()
	block.Mark(0, Done, 5*time.Second, "")

	if got := rowsFromOutput(output)[0]; !strings.Contains(got, style.Spinner("5s")) {
		t.Errorf("expected the time it took, got %q", got)
	}
}

func TestRunningTimerUsesWholeSecondGranularity(t *testing.T) {
	block := &Block{revealed: true}
	item := row{state: Running, startedAt: time.Now().Add(-5500 * time.Millisecond)}

	if got := style.Plain(block.outcome(item)); got != "✦· 5s" {
		t.Errorf("expected a whole-second timer, got %q", got)
	}
}

func TestACallThatFailedIsMarkedApartFromOneThatDidNot(t *testing.T) {
	block, output := testBlock()

	block.Add(callLabel("read", "main.go"))
	block.Add(callLabel("read", "nowhere.go"))

	block.Mark(0, Done, 0, "")
	output.Reset()
	block.Mark(1, Failed, 0, "")

	rows := rowsFromOutput(output)

	if !strings.Contains(rows[0], "✓") || !strings.Contains(rows[1], "✗") {
		t.Errorf("expected a tick and a cross, got %q", rows)
	}
}

func TestACallThatFailedSaysWhy(t *testing.T) {
	block, output := testBlock()

	block.Add(callLabel("write", "main.go"))

	output.Reset()
	block.Mark(0, Failed, 0, "permission denied")

	if got := rowsFromOutput(output)[0]; !strings.Contains(got, style.Failure("permission denied")) {
		t.Errorf("expected the reason in the colour of a failure, got %q", got)
	}
}

func TestAReasonIsPutOnTheOneRow(t *testing.T) {
	block, output := testBlock()

	block.Add(callLabel("bash", "make"))

	output.Reset()
	block.Mark(0, Failed, 0, "no rule to make target\n\tstop.\r")

	if got := rowsFromOutput(output)[0]; strings.ContainsAny(got, "\n\r\t") {
		t.Errorf("expected one row, got %q", got)
	}
}

func TestAReasonTakesTheRoomTheCallDoesNotWant(t *testing.T) {
	block, output := testBlock()

	block.Add(callLabel("read", "main.go"))

	reason := strings.Repeat("a", wide/2+10) // more than its share, less than the row has spare

	output.Reset()
	block.Mark(0, Failed, 0, reason)

	if got := rowsFromOutput(output)[0]; !strings.Contains(got, reason) {
		t.Errorf("expected the whole reason, got %q", got)
	}
}

func TestALongReasonLeavesTheCallItIsAbout(t *testing.T) {
	block, output := testBlock()

	block.Add(callLabel("write", "main.go"))

	output.Reset()
	block.Mark(0, Failed, 0, strings.Repeat("wordy ", wide))

	row := rowsFromOutput(output)[0]

	if style.Width(row) > wide {
		t.Errorf("expected the row to fit in %d columns, got %d in %q", wide, style.Width(row), row)
	}

	if !strings.Contains(row, "main.go") {
		t.Errorf("expected the call to survive the reason, got %q", row)
	}
}

func TestAReasonIsKeptOnlyForAFailure(t *testing.T) {
	block, output := testBlock()

	block.Add(callLabel("read", "main.go"))

	output.Reset()
	block.Mark(0, Done, 0, "read 40 lines")

	if got := rowsFromOutput(output)[0]; strings.Contains(got, "read 40 lines") {
		t.Errorf("expected nothing beside the mark, got %q", got)
	}
}

func TestClosingTheBlockMarksWhateverWasStillRunning(t *testing.T) {
	block, output := testBlock()

	block.Add(callLabel("read", "main.go"))
	block.Add(callLabel("grep", "spinner"))
	block.Mark(0, Done, 0, "")

	output.Reset()
	block.Close(Cancelled)

	rows := rowsFromOutput(output)

	if !strings.Contains(rows[0], "✓") {
		t.Errorf("expected the call that finished to keep its tick, got %q", rows[0])
	}

	if !strings.Contains(rows[1], "–") {
		t.Errorf("expected the call still running to be marked cancelled, got %q", rows[1])
	}
}

func TestARowIsMarkedOnlyOnce(t *testing.T) {
	block, output := testBlock()

	block.Add(callLabel("read", "main.go"))
	block.Mark(0, Failed, 0, "")

	output.Reset()
	block.Mark(0, Done, 0, "")

	if got := output.String(); got != "" {
		t.Errorf("expected nothing to be written again, got %q", got)
	}
}

func TestALabelIsCutToLeaveRoomForTheOutcome(t *testing.T) {
	block, output := testBlock()

	block.Add(callLabel("grep", strings.Repeat("x", wide)))

	output.Reset()
	block.Mark(0, Done, 3*time.Second, "")

	for _, row := range rowsFromOutput(output) {
		if style.Width(row) > wide {
			t.Errorf("expected the row to fit %d columns, got %d in %q", wide, style.Width(row), row)
		}
	}
}

func TestALabelTakesTheRoomATimeWouldHaveTakenWhereNoTimeIsShown(t *testing.T) {
	block, output := testBlock()

	block.Add(callLabel("bash", strings.Repeat("x", wide)))

	output.Reset()
	block.Mark(0, Done, time.Millisecond, "")

	for _, row := range rowsFromOutput(output) {
		if style.Width(row) != wide-edgeGuard {
			t.Errorf(
				"expected the row to leave its %d-column edge guard, got width %d in %q",
				edgeGuard, style.Width(row), row,
			)
		}
	}
}

func TestALabelGivesRoomBackWhenTheTimeAppears(t *testing.T) {
	block, output := testBlock()

	block.Add(callLabel("bash", strings.Repeat("x", wide)))

	output.Reset()
	block.Mark(0, Done, 3*time.Second, "")

	for _, row := range rowsFromOutput(output) {
		if style.Width(row) != wide-edgeGuard {
			t.Errorf(
				"expected the row to leave its %d-column edge guard, got width %d in %q",
				edgeGuard, style.Width(row), row,
			)
		}
	}
}

func TestACompletedOutcomeIsKeptBackFromTheTerminalEdge(t *testing.T) {
	block, output := testBlock()
	block.columns = 67

	block.Add(callLabel("grep", "RESOLVE_UNIX|resolve_unix|unix.*socket|Landlock|landlock"))

	output.Reset()
	block.Mark(0, Done, 7420*time.Millisecond, "")

	row := rowsFromOutput(output)[0]
	if got := style.Plain(row); !strings.HasSuffix(got, "✓ 7.4s") {
		t.Errorf("expected the complete outcome at the end, got %q", got)
	}
	if got := style.Width(row); got > block.columns-edgeGuard {
		t.Errorf("expected %d guarded columns at the edge, got width %d in %q", edgeGuard, got, row)
	}
}

func TestFormatDuration(t *testing.T) {
	for want, took := range map[string]time.Duration{
		"0.0s":   0,
		"0.9s":   999 * time.Millisecond,
		"1.2s":   1200 * time.Millisecond,
		"59.9s":  59*time.Second + 999*time.Millisecond,
		"1m00s":  time.Minute,
		"12m34s": 12*time.Minute + 34*time.Second,
		"1h40m":  100 * time.Minute,
		"4d04h":  100 * time.Hour,
	} {
		if got := util.FormatDuration(took); got != want {
			t.Errorf("expected %q, got %q", want, got)
		}

		if len(want) > durationWidth {
			t.Errorf("expected %q to fit the column of %d", want, durationWidth)
		}
	}
}
