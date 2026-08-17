package main

import (
	"context"
	"strings"
	"testing"

	"crdx.org/io/cmd/oh/theme"
	"crdx.org/io/internal/sandbox"
	"crdx.org/io/tool"
)

func TestTheBannerStartsWithAPermanentActivitySegment(t *testing.T) {
	idle := banner("gpt-5.6-sol", "high", "/tmp/io", nil, false, false, false, false, 0, false)
	if want := theme.Withheld("⠶"); !strings.HasPrefix(idle, want) {
		t.Errorf("expected the idle banner to start with %q, got %q", want, idle)
	}

	running := banner("gpt-5.6-sol", "high", "/tmp/io", nil, false, false, false, false, 1, true)
	if want := theme.Spinner("⠴"); !strings.HasPrefix(running, want) {
		t.Errorf("expected the running banner to start with %q, got %q", want, running)
	}

	if theme.Width(idle) != theme.Width(running) {
		t.Errorf("expected idle and running banners to have the same width, got %d and %d", theme.Width(idle), theme.Width(running))
	}
}

func TestTheBannerSitsAtTheLeftOfTheBottomRule(t *testing.T) {
	got := theme.Plain(bannerRule(40, "⠶ ─ io ─ gpt", "↓ 2"))
	if !strings.HasPrefix(got, "── ⠶ ─ io ─ gpt ") {
		t.Errorf("expected the banner at the left, got %q", got)
	}
	if !strings.HasSuffix(got, " ↓ 2 ──") {
		t.Errorf("expected the scroll marker at the right, got %q", got)
	}
}

func TestTheBottomRuleKeepsTheBannerBeforeItsScrollMarker(t *testing.T) {
	got := theme.Plain(bannerRule(20, "⠶ ─ io ─ gpt", "↓ 200"))
	if !strings.Contains(got, "⠶ ─ io ─ gpt") {
		t.Errorf("expected the banner to survive, got %q", got)
	}
	if strings.Contains(got, "200") {
		t.Errorf("expected the scroll marker to give way, got %q", got)
	}
}

// What access is on offer comes from what the tools say of themselves, since a world names its
// tools whatever it likes and a name is no account of what one does.
func TestAccessComesFromTheToolsRatherThanTheirNames(t *testing.T) {
	looker := tool.ReadOnly(tool.Define("peek", "", tool.Schema{},
		func(struct{}) (string, string) { return "", "" },
		func(context.Context, struct{}) (string, error) { return "", nil },
	))

	want := theme.Read("r") + theme.Withheld("w") + theme.Withheld("x") + theme.Withheld("g") + theme.Withheld("b")
	if got := modes([]tool.Tool{looker}, false, false, false, false); got != want {
		t.Errorf("expected reading alone to be on offer, got %q", got)
	}
}

// Running a command is its own kind of access, and is not implied by a tool that writes.
func TestTheShellIsShownSeparatelyFromWriting(t *testing.T) {
	writer := tool.Define("poke", "", tool.Schema{},
		func(struct{}) (string, string) { return "", "" },
		func(context.Context, struct{}) (string, error) { return "", nil },
	)

	want := theme.Withheld("r") + theme.Write("w") + theme.Exec("x") + theme.Withheld("g") + theme.Withheld("b")
	if got := modes([]tool.Tool{writer}, true, false, false, false); got != want {
		t.Errorf("expected writing and running to be on offer, got %q", got)
	}
}

// A shell granted nowhere to write changes nothing, so it must not light the writing letter.
func TestAReadOnlyShellDoesNotOfferWriting(t *testing.T) {
	processes := sandbox.NewProcesses(false)
	defer func() { _, _ = processes.Disable() }()

	mode := NewMode(capRead | capShell)
	shell := confinedShell(t.TempDir(), t.TempDir(), t.TempDir(), mode, nil, processes)

	if !shell.ReadOnly() {
		t.Errorf("expected a shell with no writable path to change nothing")
	}

	want := theme.Read("r") + theme.Withheld("w") + theme.Exec("x") + theme.Withheld("g") + theme.Withheld("b")
	if got := modes([]tool.Tool{shell}, true, false, false, false); got != want {
		t.Errorf("expected reading and running to be on offer, got %q", got)
	}
}

// Whether a repository's own history may be changed is the mode's to say rather than any tool's:
// the tools report what they may do to a file in the workspace and nothing at all about .git.
func TestTheHistoryLetterComesFromTheMode(t *testing.T) {
	looker := tool.ReadOnly(tool.Define("peek", "", tool.Schema{},
		func(struct{}) (string, string) { return "", "" },
		func(context.Context, struct{}) (string, error) { return "", nil },
	))

	want := theme.Read("r") + theme.Withheld("w") + theme.Withheld("x") + theme.History("g") + theme.Withheld("b")
	if got := modes([]tool.Tool{looker}, false, true, false, false); got != want {
		t.Errorf("expected a commit-only mode to light the history letter alone, got %q", got)
	}
}

func TestTheBackgroundLetterComesFromTheMode(t *testing.T) {
	looker := tool.ReadOnly(tool.Define("peek", "", tool.Schema{},
		func(struct{}) (string, string) { return "", "" },
		func(context.Context, struct{}) (string, error) { return "", nil },
	))

	want := theme.Read("r") + theme.Withheld("w") + theme.Withheld("x") +
		theme.Withheld("g") + theme.Background("b")
	if got := modes([]tool.Tool{looker}, false, false, true, false); got != want {
		t.Errorf("expected background mode to light its letter, got %q", got)
	}
}

// A prefix that changed nothing on the screen would be a prefix nobody could tell they had pressed,
// so every letter is underlined while one waits on the key that names what it does.
func TestAWaitingPrefixUnderlinesEveryLetter(t *testing.T) {
	looker := tool.ReadOnly(tool.Define("peek", "", tool.Schema{},
		func(struct{}) (string, string) { return "", "" },
		func(context.Context, struct{}) (string, error) { return "", nil },
	))

	got := modes([]tool.Tool{looker}, true, false, false, true)

	if underlineCount := strings.Count(got, "\x1b[4m"); underlineCount != len(capFlags) {
		t.Errorf("expected %d letters underlined, got %d in %q", len(capFlags), underlineCount, got)
	}

	if theme.Width(got) != len(capFlags) {
		t.Errorf("expected the underline to cost no cells, got %d", theme.Width(got))
	}
}

// The rule is a row of the input as far as the screen is concerned, so a rule that measures wider
// than the terminal would wrap and take the input's rows down with it.
func TestTheRuleIsExactlyAsWideAsTheScreen(t *testing.T) {
	for _, width := range []int{0, 1, 40, 100} {
		for _, label := range []string{"", "gpt ⠶ 6 tools ⠶ io", strings.Repeat("wide", 40)} {
			if got := theme.Width(rule(width, "", label)); got != width {
				t.Errorf("expected a rule of %d columns, got %d", width, got)
			}
		}
	}
}

func TestTheLabelSitsAtTheRightHandEndOfTheRule(t *testing.T) {
	ruleText := rule(20, "", "here")

	if want := " " + theme.Subtle("here") + " " + theme.Rule("──"); !strings.HasSuffix(ruleText, want) {
		t.Errorf("expected %q to end in %q", ruleText, want)
	}
}

// A rule carrying a label at either end is still exactly as wide as the screen.
func TestARuleWithBothLabelsIsExactlyAsWideAsTheScreen(t *testing.T) {
	for _, width := range []int{0, 1, 20, 40, 100} {
		if got := theme.Width(rule(width, "↑ 12", "gpt ⠶ io")); got != width {
			t.Errorf("expected a rule of %d columns, got %d", width, got)
		}
	}
}

// The right-hand label says what the harness is, so the left is the one to go when both cannot fit.
func TestTheLeftLabelIsDroppedFirst(t *testing.T) {
	ruleText := rule(18, "↑ 12", "gpt ⠶ io")

	if strings.Contains(ruleText, "12") {
		t.Errorf("expected the left label to be dropped, got %q", ruleText)
	}

	if !strings.Contains(ruleText, "gpt ⠶ io") {
		t.Errorf("expected the right label to be kept, got %q", ruleText)
	}
}

// A label with no room for it is dropped rather than pushing the rule wider than the screen.
func TestALabelTooWideForTheScreenIsDropped(t *testing.T) {
	if ruleText := rule(5, "", "far too long"); strings.Contains(ruleText, "far") {
		t.Errorf("expected the label to be dropped, got %q", ruleText)
	}
}
