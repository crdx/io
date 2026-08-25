package startup

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/prompt"
	"crdx.org/io/cmd/oh/style"
)

func TestAStartupEventKeepsItsFactsForReplay(t *testing.T) {
	info := Info{
		Session:       "brave-otter",
		ContextFiles:  []File{{Name: "SYSTEM.md", Bytes: 740}},
		ProjectSkills: 2,
		GlobalSkills:  3,
		Snippets:      4,
		ToolBytes:     2273,
	}

	event := NewEvent(12*time.Millisecond, info)
	got := style.Plain(RenderEvent(event))
	want := "New session brave-otter started in 12ms with 5 skills and 4 snippets.\n" +
		"Context: SYSTEM.md ~200t tools ~600t"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMalformedStartupFactsRenderNothing(t *testing.T) {
	event := agent.Event{Kind: agent.StartupEvent, State: json.RawMessage("{")}
	if got := RenderEvent(event); got != "" {
		t.Errorf("got %q, want nothing", got)
	}
}

func TestTookReportsTheScaleAStartupHappensOn(t *testing.T) {
	for elapsed, want := range map[time.Duration]string{
		400 * time.Microsecond:  "400µs",
		12 * time.Millisecond:   "12ms",
		1500 * time.Millisecond: "1.5s",
	} {
		if got := timeTaken(elapsed); got != want {
			t.Errorf("took(%v) = %q, want %q", elapsed, got, want)
		}
	}
}

func TestTheStartupBannerReadsAsASentenceWithItsContextBelow(t *testing.T) {
	line := RenderBanner(12*time.Millisecond, false, Info{
		Session: "brave-otter",
		ContextFiles: FilesOf([]prompt.File{
			{Name: "SYSTEM.md", Body: strings.Repeat("x", 740)},
			{Name: "AGENTS.md", Body: strings.Repeat("x", 3*1024)},
		}),
		ProjectSkills: 2,
		GlobalSkills:  3,
		Snippets:      4,
		ToolBytes:     2273,
	})
	want := "New session brave-otter started in 12ms with 5 skills and 4 snippets.\n" +
		"Context: SYSTEM.md ~200t AGENTS.md ~800t tools ~600t"

	if plainText := style.Plain(line); plainText != want {
		t.Errorf("got %q, want %q", plainText, want)
	}
}

func TestStartupQuantitiesPutOnlyTheirNumbersInTheNormalForeground(t *testing.T) {
	for name, test := range map[string]struct {
		quantity   string
		unitNormal bool
		want       string
	}{
		"duration":       {"355µs", false, style.Normal("355") + style.Subtle("µs")},
		"file size":      {"1G", true, style.Normal("1G")},
		"token estimate": {"~1.22Kt", false, style.Subtle("~") + style.Normal("1.22") + style.Subtle("Kt")},
		"no number":      {"none", false, style.Subtle("none")},
	} {
		t.Run(name, func(t *testing.T) {
			var line startupLine
			line.quantity(test.quantity, test.unitNormal)
			if got := line.String(); got != test.want {
				t.Errorf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestAnEmptyStartupSaysTheSentenceAlone(t *testing.T) {
	line := style.Plain(RenderBanner(time.Millisecond, false, Info{}))
	want := "New session started in 1ms with 0 skills and 0 snippets."

	if line != want {
		t.Errorf("got %q, want %q", line, want)
	}
}

func TestTheContextLineListsOnlyWhatThereIs(t *testing.T) {
	for name, test := range map[string]struct {
		info Info
		want string
	}{
		"files only": {
			info: Info{ContextFiles: []File{{Name: "SYSTEM.md", Bytes: 740}}},
			want: "Context: SYSTEM.md ~200t",
		},
		"tools only": {
			info: Info{ToolBytes: 2320},
			want: "Context: tools ~600t",
		},
	} {
		t.Run(name, func(t *testing.T) {
			line := style.Plain(RenderBanner(time.Millisecond, false, test.info))
			if !strings.HasSuffix(line, "\n"+test.want) {
				t.Errorf("got %q, want it to end with %q", line, "\n"+test.want)
			}
		})
	}
}

func TestAResumedConversationHasNoStartupLine(t *testing.T) {
	line := RenderBanner(time.Millisecond, true, Info{ProjectSkills: 2})

	if line != "" {
		t.Errorf("expected no startup line, got %q", line)
	}
}
