package main

import (
	"strings"
	"testing"
	"time"

	"crdx.org/io/cmd/oh/prompt"
	"crdx.org/io/cmd/oh/style"
)

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

func TestTheStartupLineUsesTheCompactSummary(t *testing.T) {
	line := renderStartupBanner(12*time.Millisecond, false, startupInfo{
		Session: "brave-otter",
		ContextFiles: startupFilesOf([]prompt.File{
			{Name: "SYSTEM.md", Body: strings.Repeat("x", 740)},
			{Name: "AGENTS.md", Body: strings.Repeat("x", 3*1024)},
		}),
		ProjectSkills: 2,
		GlobalSkills:  3,
		ToolBytes:     2273,
	})
	want := "startup=12ms session=brave-otter SYSTEM.md=~200t AGENTS.md=~800t skills=2p/3g tools=~600t"

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

func TestNoPromptFilesLeaveNoEmptyField(t *testing.T) {
	line := style.Plain(renderStartupBanner(time.Millisecond, false, startupInfo{}))
	want := "startup=1ms skills=0p/0g tools=0t"

	if line != want {
		t.Errorf("got %q, want %q", line, want)
	}
}

func TestAResumedConversationHasNoStartupLine(t *testing.T) {
	line := renderStartupBanner(time.Millisecond, true, startupInfo{ProjectSkills: 2})

	if line != "" {
		t.Errorf("expected no startup line, got %q", line)
	}
}
