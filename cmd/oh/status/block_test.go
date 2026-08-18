package status

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"crdx.org/io/cmd/oh/markdown"
	"crdx.org/io/cmd/oh/theme"
	"crdx.org/io/internal/util"
	"crdx.org/io/tool"
)

const wide = 100

func TestMeasuredStatisticsAreShownAfterCalls(t *testing.T) {
	for name, test := range map[string]struct {
		stats tool.Statistics
		want  []string
	}{
		"resources": {
			stats: tool.Statistics{
				Kind: tool.StatsResources, CPUTime: 800 * time.Millisecond, PeakMemory: 92 << 20, Lines: 7, Bytes: 1200,
			},
			want: []string{"~300t 7L 1.4s 0.8s 92M"},
		},
		"read": {
			stats: tool.Statistics{Kind: tool.StatsRead, Lines: 42, Bytes: 1200},
			want:  []string{"42L ~300t"},
		},
		"image": {
			stats: tool.Statistics{Kind: tool.StatsImage, Bytes: 80_943, EstimatedTokens: 1536},
			want:  []string{"~1.5Kt"},
		},
		"write": {
			stats: tool.Statistics{Kind: tool.StatsWrite, Lines: 3, Bytes: 17},
			want:  []string{"3L ~5t"},
		},
		"diff": {
			stats: tool.Statistics{Kind: tool.StatsDiff, Added: 3, Removed: 2},
			want:  []string{"+3 −2"},
		},
		"search": {
			stats: tool.Statistics{Kind: tool.StatsSearch, Lines: 17, Bytes: 1200},
			want:  []string{"17L ~300t"},
		},
		"capped search": {
			stats: tool.Statistics{
				Kind: tool.StatsSearch, Lines: 100, Bytes: 32_000, TotalBytes: 80_000, Truncated: true,
			},
			want: []string{"100L+ ~8Kt (of ~20Kt)"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			got := theme.Plain(outcomeText("✓", 1400*time.Millisecond, &test.stats))
			for _, want := range test.want {
				if !strings.Contains(got, want) {
					t.Errorf("got %q, want %q", got, want)
				}
			}
		})
	}
}

func TestStatisticsUseTheirExpectedStyles(t *testing.T) {
	read := outcomeText("✓", 0, &tool.Statistics{Kind: tool.StatsRead, Lines: 45, Bytes: 951})
	if want := theme.Detail("45L ~200t"); !strings.Contains(read, want) {
		t.Errorf("read statistics got %q, want styled %q", read, want)
	}

	write := outcomeText("✓", 0, &tool.Statistics{Kind: tool.StatsWrite, Lines: 12, Bytes: 1200})
	if want := theme.Detail("12L ~300t"); !strings.Contains(write, want) {
		t.Errorf("write statistics got %q, want styled %q", write, want)
	}

	search := outcomeText("✓", 0, &tool.Statistics{
		Kind: tool.StatsSearch, Lines: 23, Bytes: 1200, TotalBytes: 2400, Truncated: true,
	})
	if want := theme.Detail("23L+ ~300t (of ~600t)"); !strings.Contains(search, want) {
		t.Errorf("search statistics got %q, want styled %q", search, want)
	}

	exec := outcomeText("✓", 0, &tool.Statistics{
		Kind: tool.StatsResources, PeakMemory: 26 << 20,
	})
	wantExec := theme.Detail("0t 0L 0s 0s 26M")
	if !strings.Contains(exec, wantExec) {
		t.Errorf("exec statistics got %q, want styled %q", exec, wantExec)
	}

	edit := outcomeText("✓", 0, &tool.Statistics{Kind: tool.StatsDiff, Added: 2, Removed: 1})
	wantEdit := theme.Success("+2") + theme.Detail(" ") + theme.Failure("−1")
	if !strings.Contains(edit, wantEdit) {
		t.Errorf("edit statistics got %q, want styled %q", edit, wantEdit)
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

func callLabel(name string, args string) Label {
	return Label{Name: name, Args: args, ReadOnly: true}
}

var rise = regexp.MustCompile(`^\x1b\[\d+A`)

func rowsFromOutput(output *strings.Builder) []string {
	text := rise.ReplaceAllString(output.String(), "")

	return strings.Split(strings.ReplaceAll(text, "\r\x1b[K", ""), "\n")
}

func TestOnlyTheFocusedPartOfArgumentsIsPainted(t *testing.T) {
	label := Label{Name: "read", Args: "cmd/oh/draw.go", Focus: "draw.go", ReadOnly: true}
	want := theme.Call("read") + " " + theme.Detail("cmd/oh/") + theme.Args("draw.go")

	if got := label.render(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAnAccentAndTheFocusedPartOfArgumentsArePainted(t *testing.T) {
	label := Label{
		Name:        "skill",
		NameStyle:   theme.Skill,
		Args:        "/skills/guard-basics/SKILL.md",
		Focus:       "SKILL.md",
		Accent:      "guard-basics",
		AccentStyle: theme.Skill,
	}
	want := theme.Skill("skill") + " " +
		theme.Detail("/skills/") + theme.Skill("guard-basics") +
		theme.Detail("/") + theme.Args("SKILL.md")

	if got := label.render(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestArgumentsWithSyntaxAreHighlighted(t *testing.T) {
	label := Label{Name: "bash", Args: "echo one && true", Syntax: "bash"}
	want := theme.Change("bash") + " " + markdown.Highlight(label.Args, "bash")

	if got := label.render(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAPathInTheDetailCanBeFocused(t *testing.T) {
	label := Label{Name: "grep", Args: "text", Detail: "in cmd/oh/draw.go", Focus: "draw.go"}
	want := theme.Change("grep") + " " + theme.Args("text") + " " +
		theme.Detail("in cmd/oh/") + theme.Args("draw.go")

	if got := label.render(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// A call gets a while to finish before its row says it is still going, so a block that finishes
// quickly fills in without a spinner ever appearing on it.
func TestARowSaysNothingOfACallUntilItHasBeenGoingAWhile(t *testing.T) {
	block, output := testBlock()

	block.Add(callLabel("read", "main.go"))

	if got := output.String(); got != theme.Call("read")+" "+theme.Args("main.go") {
		t.Errorf("expected the call and nothing else, got %q", got)
	}
}

// A row is coloured by what its call may do, so a call that changes something is apart from one
// that only looks before either has finished.
func TestARowIsColouredByWhetherItsCallWrites(t *testing.T) {
	block, output := testBlock()

	block.Add(Label{Name: "write", Args: "main.go"})

	if got := output.String(); got != theme.Change("write")+" "+theme.Args("main.go") {
		t.Errorf("expected a call that writes to be painted as one, got %q", got)
	}
}

// A call that came back before there was any waiting to speak of says only that it is done. A time
// against every row of a block that filled in at once is a number standing where nothing needs
// saying.
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

	if got := rowsFromOutput(output)[0]; !strings.Contains(got, theme.Spinner("5.0s")) {
		t.Errorf("expected the time it took, got %q", got)
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

// A cross says a call failed and nothing else, so what it said for itself is put beside it. A
// failure the model was told about and the person was not is a row that looks like a typo.
func TestACallThatFailedSaysWhy(t *testing.T) {
	block, output := testBlock()

	block.Add(callLabel("write", "main.go"))

	output.Reset()
	block.Mark(0, Failed, 0, "permission denied")

	if got := rowsFromOutput(output)[0]; !strings.Contains(got, theme.Failure("permission denied")) {
		t.Errorf("expected the reason in the colour of a failure, got %q", got)
	}
}

// A reason arrives as whatever the tool wrote, which may be a paragraph. A row is drawn where the
// last one left off, so a newline in it would take the whole block apart.
func TestAReasonIsPutOnTheOneRow(t *testing.T) {
	block, output := testBlock()

	block.Add(callLabel("bash", "make"))

	output.Reset()
	block.Mark(0, Failed, 0, "no rule to make target\n\tstop.\r")

	if got := rowsFromOutput(output)[0]; strings.ContainsAny(got, "\n\r\t") {
		t.Errorf("expected one row, got %q", got)
	}
}

// A short call leaves most of the row unused, and a reason that fits in what is left is worth more
// there than the blank would be.
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

// The call is what the row is about, so a reason long enough to bury it is cut down to size first.
func TestALongReasonLeavesTheCallItIsAbout(t *testing.T) {
	block, output := testBlock()

	block.Add(callLabel("write", "main.go"))

	output.Reset()
	block.Mark(0, Failed, 0, strings.Repeat("wordy ", wide))

	row := rowsFromOutput(output)[0]

	if theme.Width(row) > wide {
		t.Errorf("expected the row to fit in %d columns, got %d in %q", wide, theme.Width(row), row)
	}

	if !strings.Contains(row, "main.go") {
		t.Errorf("expected the call to survive the reason, got %q", row)
	}
}

// Nothing to say is nothing said: a call that worked carries no reason, and neither does the row.
func TestAReasonIsKeptOnlyForAFailure(t *testing.T) {
	block, output := testBlock()

	block.Add(callLabel("read", "main.go"))

	output.Reset()
	block.Mark(0, Done, 0, "read 40 lines")

	if got := rowsFromOutput(output)[0]; strings.Contains(got, "read 40 lines") {
		t.Errorf("expected nothing beside the mark, got %q", got)
	}
}

// A block closed while calls are still running says so on their rows, rather than leaving them
// looking like calls still going when nothing is.
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

// A row is settled by the first thing said about it, so closing a block whose calls have all
// reported cannot write over what they reported.
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

// A label that would push the outcome off the screen gives way to it, since a row running past the
// width takes every row under it down with it.
func TestALabelIsCutToLeaveRoomForTheOutcome(t *testing.T) {
	block, output := testBlock()

	block.Add(callLabel("grep", strings.Repeat("x", wide)))

	output.Reset()
	block.Mark(0, Done, 3*time.Second, "")

	for _, row := range rowsFromOutput(output) {
		if theme.Width(row) > wide {
			t.Errorf("expected the row to fit %d columns, got %d in %q", wide, theme.Width(row), row)
		}
	}
}

// Room kept back for a time that is never shown is room the label could have had, so a call that
// finished too quickly to be timed gives it back, bar the mark and the uncertain terminal edge.
func TestALabelTakesTheRoomATimeWouldHaveTakenWhereNoTimeIsShown(t *testing.T) {
	block, output := testBlock()

	block.Add(callLabel("bash", strings.Repeat("x", wide)))

	output.Reset()
	block.Mark(0, Done, time.Millisecond, "")

	for _, row := range rowsFromOutput(output) {
		if theme.Width(row) != wide-edgeGuard {
			t.Errorf(
				"expected the row to leave its %d-column edge guard, got width %d in %q",
				edgeGuard, theme.Width(row), row,
			)
		}
	}
}

// The label gives room back the moment the time appears beside it, rather than the row growing past
// the guarded width and taking every row under it down with it.
func TestALabelGivesRoomBackWhenTheTimeAppears(t *testing.T) {
	block, output := testBlock()

	block.Add(callLabel("bash", strings.Repeat("x", wide)))

	output.Reset()
	block.Mark(0, Done, 3*time.Second, "")

	for _, row := range rowsFromOutput(output) {
		if theme.Width(row) != wide-edgeGuard {
			t.Errorf(
				"expected the row to leave its %d-column edge guard, got width %d in %q",
				edgeGuard, theme.Width(row), row,
			)
		}
	}
}

// Some terminals draw the symbols in a cut, completed row wider than the width tables say. The
// uncertain cells are taken from the label rather than letting the right edge eat the duration.
func TestACompletedOutcomeIsKeptBackFromTheTerminalEdge(t *testing.T) {
	block, output := testBlock()
	block.columns = 67

	block.Add(callLabel("grep", "RESOLVE_UNIX|resolve_unix|unix.*socket|Landlock|landlock"))

	output.Reset()
	block.Mark(0, Done, 7420*time.Millisecond, "")

	row := rowsFromOutput(output)[0]
	if got := theme.Plain(row); !strings.HasSuffix(got, "✓ 7.4s") {
		t.Errorf("expected the complete outcome at the end, got %q", got)
	}
	if got := theme.Width(row); got > block.columns-edgeGuard {
		t.Errorf("expected %d guarded columns at the edge, got width %d in %q", edgeGuard, got, row)
	}
}

// Every form the time takes, none of them wider than the column kept for it.
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
