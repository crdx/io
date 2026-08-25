package startup

import (
	"encoding/json"
	"testing"
	"time"

	"crdx.org/io/agent"
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
	want := "Session brave-otter started in 12ms with 5 skills, 4 snippets, and ~800t of context."
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
	want := "Session started in 1ms with 0 skills, 0 snippets, and 0t of context."

	if line != want {
		t.Errorf("got %q, want %q", line, want)
	}
}

func TestAResumedConversationHasNoStartupLine(t *testing.T) {
	line := RenderBanner(time.Millisecond, true, Info{ProjectSkills: 2})

	if line != "" {
		t.Errorf("expected no startup line, got %q", line)
	}
}
