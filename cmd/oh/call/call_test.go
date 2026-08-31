package call

import (
	"strings"
	"testing"
	"time"

	"crdx.org/io/cmd/oh/markdown"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/oh/width"
	"crdx.org/io/internal/util"
	"crdx.org/io/tool"
)

func elided(t *testing.T, label Label, room int) Label {
	t.Helper()

	elidedLabel, isLabel := label.Elide(room).(Label)
	if !isLabel {
		t.Fatalf("expected a label, got %T", elidedLabel)
	}

	return elidedLabel
}

func measured(stats *tool.Stats) string {
	text := Measurements(stats)
	if text == "" {
		return "✓"
	}

	return "✓ " + text
}

func TestStatsAreShownAfterCalls(t *testing.T) {
	for name, test := range map[string]struct {
		stats tool.Stats
		want  []string
	}{
		"output": {
			stats: tool.Stats{Kind: tool.StatsOutput, Lines: 4, Bytes: 1200, TotalBytes: 1200},
			want:  []string{"4L ~400t"},
		},
		"empty output": {
			stats: tool.Stats{Kind: tool.StatsOutput},
			want:  []string{"no output"},
		},
		"capped output": {
			stats: tool.Stats{
				Kind:        tool.StatsOutput,
				Lines:       4,
				Bytes:       1200,
				TotalBytes:  1200,
				IsTruncated: true,
			},
			want: []string{"4L+ ~400t"},
		},
		"resources": {
			stats: tool.Stats{
				Kind:       tool.StatsResources,
				CPUTime:    800 * time.Millisecond,
				PeakMemory: 92 << 20,
				Lines:      7,
				Bytes:      1200,
			},
			want: []string{"7L ~400t 0.8s 92M"},
		},
		"resources spent on nothing": {
			stats: tool.Stats{Kind: tool.StatsResources},
			want:  []string{"no output"},
		},
		"resources without a cost worth naming": {
			stats: tool.Stats{Kind: tool.StatsResources, Lines: 3, Bytes: 40},
			want:  []string{"3L"},
		},
		"read": {
			stats: tool.Stats{Kind: tool.StatsRead, Lines: 42, Bytes: 1200},
			want:  []string{"42L ~400t"},
		},
		"list": {
			stats: tool.Stats{Kind: tool.StatsList, Lines: 42},
			want:  []string{"42L"},
		},
		"image": {
			stats: tool.Stats{Kind: tool.StatsImage, Bytes: 80_943, EstimatedTokens: 1536},
			want:  []string{"~1.5Kt"},
		},
		"small estimate hidden": {
			stats: tool.Stats{Kind: tool.StatsWrite, Lines: 3, Bytes: 280},
			want:  []string{"3L"},
		},
		"estimate above threshold shown": {
			stats: tool.Stats{Kind: tool.StatsWrite, Lines: 3, Bytes: 281},
			want:  []string{"3L ~100t"},
		},
		"diff": {
			stats: tool.Stats{Kind: tool.StatsDiff, AddedLines: 3, RemovedLines: 2},
			want:  []string{"+3 −2"},
		},
		"search": {
			stats: tool.Stats{Kind: tool.StatsSearch, Lines: 17, Bytes: 1200},
			want:  []string{"17L ~400t"},
		},
		"small capped output with a large total": {
			stats: tool.Stats{
				Kind:        tool.StatsSearch,
				Lines:       2,
				Bytes:       280,
				TotalBytes:  1200,
				IsTruncated: true,
			},
			want: []string{"2L+ (of ~400t)"},
		},
		"capped search": {
			stats: tool.Stats{
				Kind:        tool.StatsSearch,
				Lines:       100,
				Bytes:       32_000,
				TotalBytes:  80_000,
				IsTruncated: true,
			},
			want: []string{"100L+ ~11Kt (of ~29Kt)"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			got := style.Plain(measured(&test.stats))
			for _, want := range test.want {
				if !strings.Contains(got, want) {
					t.Errorf("got %q, want %q", got, want)
				}
			}
		})
	}
}

func TestStatsUseTheirExpectedStyles(t *testing.T) {
	output := measured(&tool.Stats{Kind: tool.StatsOutput, Lines: 4, Bytes: 951})
	if want := style.Subtle("4L ~300t"); !strings.Contains(output, want) {
		t.Errorf("output stats got %q, want styled %q", output, want)
	}

	emptyOutput := measured(&tool.Stats{Kind: tool.StatsOutput})
	if want := style.Subtle("no output"); !strings.Contains(emptyOutput, want) {
		t.Errorf("empty output stats got %q, want styled %q", emptyOutput, want)
	}

	read := measured(&tool.Stats{Kind: tool.StatsRead, Lines: 45, Bytes: 951})
	if want := style.Subtle("45L ~300t"); !strings.Contains(read, want) {
		t.Errorf("read stats got %q, want styled %q", read, want)
	}

	write := measured(&tool.Stats{Kind: tool.StatsWrite, Lines: 12, Bytes: 1200})
	if want := style.Subtle("12L ~400t"); !strings.Contains(write, want) {
		t.Errorf("write stats got %q, want styled %q", write, want)
	}

	search := measured(&tool.Stats{
		Kind:        tool.StatsSearch,
		Lines:       23,
		Bytes:       1200,
		TotalBytes:  2400,
		IsTruncated: true,
	})
	if want := style.Subtle("23L+ ~400t (of ~900t)"); !strings.Contains(search, want) {
		t.Errorf("search stats got %q, want styled %q", search, want)
	}

	exec := measured(&tool.Stats{
		Kind:       tool.StatsResources,
		PeakMemory: 26 << 20,
	})
	wantExec := style.Subtle("no output 26M")
	if !strings.Contains(exec, wantExec) {
		t.Errorf("exec stats got %q, want styled %q", exec, wantExec)
	}

	edit := measured(&tool.Stats{Kind: tool.StatsDiff, AddedLines: 2, RemovedLines: 1})
	wantEdit := style.Success("+2") + style.Subtle(" ") + style.Failure("−1")
	if !strings.Contains(edit, wantEdit) {
		t.Errorf("edit stats got %q, want styled %q", edit, wantEdit)
	}
}

func TestOnlyTheFocusedPartOfArgumentsIsPainted(t *testing.T) {
	label := Label{
		Name:     "read",
		Subject:  "cmd/oh/draw.go",
		ReadOnly: true,
		Emphasis: tool.Emphasis{Kind: tool.EmphasisFocus, Value: "draw.go"},
	}
	want := style.Call("read") + " " + style.Subtle("cmd/oh/") + style.Subject("draw.go")

	if got := label.Render(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAnAccentAndTheFocusedPartOfArgumentsArePainted(t *testing.T) {
	label := Label{
		Name:        "skill",
		NameStyle:   style.Skill,
		Subject:     "/skills/guard-basics/SKILL.md",
		Emphasis:    tool.Emphasis{Kind: tool.EmphasisFocus, Value: "SKILL.md"},
		Accent:      "guard-basics",
		AccentStyle: style.Skill,
	}
	want := style.Skill("skill") + " " +
		style.Subtle("/skills/") + style.Skill("guard-basics") +
		style.Subtle("/") + style.Subject("SKILL.md")

	if got := label.Render(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestArgumentsWithSyntaxAreHighlighted(t *testing.T) {
	label := Label{
		Name:     "bash",
		Subject:  "echo one && true",
		Emphasis: tool.Emphasis{Kind: tool.EmphasisSyntax, Value: "bash"},
	}
	want := style.Change("bash") + " " + markdown.Emphasise(label.Subject, "bash")

	if got := label.Render(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSyntaxCanBeHighlightedFromSourceBeyondTheDisplayedSubject(t *testing.T) {
	label := Label{
		Name:    "bash",
		Subject: "cat <<'EOF'",
		Emphasis: tool.Emphasis{
			Kind:   tool.EmphasisSyntax,
			Value:  "bash",
			Source: "cat <<'EOF'\nhello\nEOF",
		},
	}
	want := style.Change("bash") + " " + style.Function("cat") + style.Block(" ") +
		style.Operator("<<") + style.Block("'EOF'")

	if got := label.Render(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestElidedBashKeepsItsEmphasisFromTheCompleteCommand(t *testing.T) {
	source := "go list ordinary"
	for name, test := range map[string]struct {
		argumentRoom int
		plain        string
		want         string
	}{
		"inside executable": {
			2,
			"g…",
			style.Function("g") + style.Function(width.Ellipsis),
		},
		"at parameter start": {
			4,
			"go …",
			style.Function("go") + style.Block(" ") + style.Function(width.Ellipsis),
		},
		"inside parameter": {
			6,
			"go li…",
			style.Function("go") + style.Block(" ") + style.Function("li") + style.Function(width.Ellipsis),
		},
		"at parameter end": {
			8,
			"go list…",
			style.Function("go") + style.Block(" ") + style.Function("list") + style.Block(width.Ellipsis),
		},
		"inside later argument": {
			12,
			"go list ord…",
			style.Function("go") + style.Block(" ") + style.Function("list") + style.Block(" ord") + style.Block(width.Ellipsis),
		},
	} {
		label := Label{
			Name:     "bash",
			Subject:  source,
			Emphasis: tool.Emphasis{Kind: tool.EmphasisSyntax, Value: "bash"},
		}
		got := elided(t, label, len("bash ")+test.argumentRoom).renderSubject()

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
		Name:     "bash",
		Subject:  "echo 日本語 later",
		Emphasis: tool.Emphasis{Kind: tool.EmphasisSyntax, Value: "bash"},
	}
	got := elided(t, label, len("bash ")+argumentRoom).renderSubject()
	want := style.Function("echo") + style.Block(" ") + style.Function("日") + style.Function(width.Ellipsis)

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
		Name:      "grep",
		Subject:   "text",
		Qualifier: "in cmd/oh/draw.go",
		Emphasis:  tool.Emphasis{Kind: tool.EmphasisFocus, Value: "draw.go"},
	}
	want := style.Change("grep") + " " + style.Subject("text") + " " +
		style.Qualifier("in cmd/oh/") + style.Subject("draw.go")

	if got := label.Render(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestALabelIsColouredByWhetherItsCallWrites(t *testing.T) {
	label := Label{Name: "write", Subject: "main.go"}

	if got := label.Render(); got != style.Change("write")+" "+style.Subject("main.go") {
		t.Errorf("expected a call that writes to be painted as one, got %q", got)
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
