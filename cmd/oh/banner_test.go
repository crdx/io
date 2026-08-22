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
