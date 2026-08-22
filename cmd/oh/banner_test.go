package main

import (
	"context"
	"strings"
	"testing"

	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/tool"
)

func TestTheBannerStartsWithAPermanentActivitySegment(t *testing.T) {
	idle := banner("gpt-5.6-sol", "high", "/tmp/io", nil, caps.Read, false, false, 0)
	if want := style.Withheld("✧·"); !strings.HasPrefix(idle, want) {
		t.Errorf("expected the idle banner to start with %q, got %q", want, idle)
	}

	running := banner("gpt-5.6-sol", "high", "/tmp/io", nil, caps.Read, false, true, 1)
	if want := style.Spinner("·✦"); !strings.HasPrefix(running, want) {
		t.Errorf("expected the running banner to start with %q, got %q", want, running)
	}

	if style.Width(idle) != style.Width(running) {
		t.Errorf("expected idle and running banners to have the same width, got %d and %d", style.Width(idle), style.Width(running))
	}
}

func TestTheBannerSitsAtTheLeftOfTheBottomRule(t *testing.T) {
	got := style.Plain(bannerRule(40, "⠶ ─ io ─ gpt", "↓ 2"))
	if !strings.HasPrefix(got, "─ ⠶ ─ io ─ gpt ") {
		t.Errorf("expected the banner at the left, got %q", got)
	}
	if !strings.HasSuffix(got, " ↓ 2 ──") {
		t.Errorf("expected the scroll marker at the right, got %q", got)
	}
}

func TestTheBottomRuleKeepsTheBannerBeforeItsScrollMarker(t *testing.T) {
	got := style.Plain(bannerRule(20, "⠶ ─ io ─ gpt", "↓ 200"))
	if !strings.Contains(got, "⠶ ─ io ─ gpt") {
		t.Errorf("expected the banner to survive, got %q", got)
	}
	if strings.Contains(got, "200") {
		t.Errorf("expected the scroll marker to give way, got %q", got)
	}
}

func TestAccessComesFromTheToolsRatherThanTheirNames(t *testing.T) {
	looker := tool.ReadOnly(tool.Implement(
		tool.Definition{
			Name:        "peek",
			Description: "",
			Schema:      tool.Schema{},
		},
		func(struct{}) (string, string) { return "", "" },
	).Plain(func(context.Context, struct{}) (string, error) { return "", nil }))

	want := style.Read("r") + style.Withheld("x") + style.Withheld("w") + style.Withheld("g") + style.Withheld("b")
	if got := modes([]tool.Tool{looker}, caps.Read, false); got != want {
		t.Errorf("expected reading alone to be on offer, got %q", got)
	}
}

func TestTheReadLetterGoesDarkWithoutAToolThatReads(t *testing.T) {
	writer := tool.Implement(
		tool.Definition{
			Name:        "poke",
			Description: "",
			Schema:      tool.Schema{},
		},
		func(struct{}) (string, string) { return "", "" },
	).Plain(func(context.Context, struct{}) (string, error) { return "", nil })

	want := style.Withheld("r") + style.Exec("x") + style.Write("w") + style.Withheld("g") + style.Withheld("b")
	if got := modes([]tool.Tool{writer}, caps.Read|caps.Shell|caps.Write, false); got != want {
		t.Errorf("expected writing and running to be on offer, got %q", got)
	}
}

func TestTheWriteLetterComesFromTheMode(t *testing.T) {
	looker := tool.ReadOnly(tool.Implement(
		tool.Definition{
			Name:        "peek",
			Description: "",
			Schema:      tool.Schema{},
		},
		func(struct{}) (string, string) { return "", "" },
	).Plain(func(context.Context, struct{}) (string, error) { return "", nil }))

	want := style.Read("r") + style.Exec("x") + style.Write("w") + style.Withheld("g") + style.Withheld("b")
	if got := modes([]tool.Tool{looker}, caps.Read|caps.Shell|caps.Write, false); got != want {
		t.Errorf("expected writing to light its letter whatever the tools are, got %q", got)
	}
}

func TestTheHistoryLetterComesFromTheMode(t *testing.T) {
	looker := tool.ReadOnly(tool.Implement(
		tool.Definition{
			Name:        "peek",
			Description: "",
			Schema:      tool.Schema{},
		},
		func(struct{}) (string, string) { return "", "" },
	).Plain(func(context.Context, struct{}) (string, error) { return "", nil }))

	want := style.Read("r") + style.Withheld("x") + style.Withheld("w") + style.History("g") + style.Withheld("b")
	if got := modes([]tool.Tool{looker}, caps.Read|caps.Git, false); got != want {
		t.Errorf("expected a commit-only mode to light the history letter alone, got %q", got)
	}
}

func TestTheBackgroundLetterComesFromTheMode(t *testing.T) {
	looker := tool.ReadOnly(tool.Implement(
		tool.Definition{
			Name:        "peek",
			Description: "",
			Schema:      tool.Schema{},
		},
		func(struct{}) (string, string) { return "", "" },
	).Plain(func(context.Context, struct{}) (string, error) { return "", nil }))

	want := style.Read("r") + style.Withheld("x") + style.Withheld("w") +
		style.Withheld("g") + style.Background("b")
	if got := modes([]tool.Tool{looker}, caps.Read|caps.Background, false); got != want {
		t.Errorf("expected background mode to light its letter, got %q", got)
	}
}

func TestAWaitingPrefixUnderlinesEveryLetter(t *testing.T) {
	looker := tool.ReadOnly(tool.Implement(
		tool.Definition{
			Name:        "peek",
			Description: "",
			Schema:      tool.Schema{},
		},
		func(struct{}) (string, string) { return "", "" },
	).Plain(func(context.Context, struct{}) (string, error) { return "", nil }))

	got := modes([]tool.Tool{looker}, caps.Read|caps.Shell, true)

	if underlineCount := strings.Count(got, "\x1b[4m"); underlineCount != len(caps.AllFlags) {
		t.Errorf("expected %d letters underlined, got %d in %q", len(caps.AllFlags), underlineCount, got)
	}

	if style.Width(got) != len(caps.AllFlags) {
		t.Errorf("expected the underline to cost no cells, got %d", style.Width(got))
	}
}

func TestTheRuleIsExactlyAsWideAsTheScreen(t *testing.T) {
	for _, width := range []int{0, 1, 40, 100} {
		for _, label := range []string{"", "gpt ⠶ 6 tools ⠶ io", strings.Repeat("wide", 40)} {
			if got := style.Width(rule(width, "", label)); got != width {
				t.Errorf("expected a rule of %d columns, got %d", width, got)
			}
		}
	}
}

func TestTheLabelSitsAtTheRightHandEndOfTheRule(t *testing.T) {
	ruleText := rule(20, "", "here")

	if want := " " + style.Subtle("here") + " " + style.Rule("──"); !strings.HasSuffix(ruleText, want) {
		t.Errorf("expected %q to end in %q", ruleText, want)
	}
}

func TestARuleWithBothLabelsIsExactlyAsWideAsTheScreen(t *testing.T) {
	for _, width := range []int{0, 1, 20, 40, 100} {
		if got := style.Width(rule(width, "↑ 12", "gpt ⠶ io")); got != width {
			t.Errorf("expected a rule of %d columns, got %d", width, got)
		}
	}
}

func TestTheLeftLabelIsDroppedFirst(t *testing.T) {
	ruleText := rule(18, "↑ 12", "gpt ⠶ io")

	if strings.Contains(ruleText, "12") {
		t.Errorf("expected the left label to be dropped, got %q", ruleText)
	}

	if !strings.Contains(ruleText, "gpt ⠶ io") {
		t.Errorf("expected the right label to be kept, got %q", ruleText)
	}
}

func TestALabelTooWideForTheScreenIsDropped(t *testing.T) {
	if ruleText := rule(5, "", "far too long"); strings.Contains(ruleText, "far") {
		t.Errorf("expected the label to be dropped, got %q", ruleText)
	}
}

func modesFor(tools []tool.Tool, grantedCaps caps.Set) string {
	return modes(tools, grantedCaps, false)
}

func TestEachCapabilityLightsItsOwnLetterAlone(t *testing.T) {
	looker := tool.ReadOnly(tool.Implement(
		tool.Definition{Name: "peek", Description: "", Schema: tool.Schema{}},
		func(struct{}) (string, string) { return "", "" },
	).Plain(func(context.Context, struct{}) (string, error) { return "", nil }))

	dark := map[string]string{
		"r": style.Withheld("r"), "x": style.Withheld("x"), "w": style.Withheld("w"),
		"g": style.Withheld("g"), "b": style.Withheld("b"),
	}

	lit := func(letters map[string]string) string {
		var out strings.Builder
		for _, flag := range []string{"r", "x", "w", "g", "b"} {
			if replacement, found := letters[flag]; found {
				out.WriteString(replacement)
			} else {
				out.WriteString(dark[flag])
			}
		}
		return out.String()
	}

	tests := map[string]struct {
		grantedCaps caps.Set
		want        string
	}{
		"reading alone": {caps.Read, lit(map[string]string{"r": style.Read("r")})},
		"the shell":     {caps.Read | caps.Shell, lit(map[string]string{"r": style.Read("r"), "x": style.Exec("x")})},
		"writing":       {caps.Read | caps.Write, lit(map[string]string{"r": style.Read("r"), "w": style.Write("w")})},
		"history":       {caps.Read | caps.Git, lit(map[string]string{"r": style.Read("r"), "g": style.History("g")})},
		"background": {
			caps.Read | caps.Background,
			lit(map[string]string{"r": style.Read("r"), "b": style.Background("b")}),
		},
		"the lot": {
			caps.Read | caps.Shell | caps.Write | caps.Git | caps.Background,
			lit(map[string]string{
				"r": style.Read("r"), "x": style.Exec("x"), "w": style.Write("w"),
				"g": style.History("g"), "b": style.Background("b"),
			}),
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if got := modesFor([]tool.Tool{looker}, test.grantedCaps); got != test.want {
				t.Errorf("got %q, want %q", got, test.want)
			}
		})
	}
}
