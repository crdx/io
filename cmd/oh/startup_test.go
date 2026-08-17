package main

import (
	"strings"
	"testing"
	"time"
)

// A startup is measured in milliseconds, which the conversation's own duration format rounds away
// to nothing.
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

func TestSpellWritesALimitTheWayItWasSet(t *testing.T) {
	for limit, want := range map[time.Duration]string{
		0:                     "none",
		5 * time.Minute:       "5m",
		60 * time.Second:      "1m",
		90 * time.Second:      "90s",
		30 * time.Millisecond: "0s",
	} {
		if got := formatDuration(limit); got != want {
			t.Errorf("spell(%v) = %q, want %q", limit, got, want)
		}
	}
}

func TestSizeWritesAFileLimitInWholeUnits(t *testing.T) {
	for limit, want := range map[int64]string{
		0:               "none",
		256 * megabyte:  "256M",
		gigabyte:        "1G",
		1536 * megabyte: "1.5G",
	} {
		if got := size(limit); got != want {
			t.Errorf("size(%d) = %q, want %q", limit, got, want)
		}
	}
}

// The limits are the same whatever is granted, so the line reports them without asking what a
// command would be confined to.
func TestTheStartupLineSaysWhatACommandIsHeldTo(t *testing.T) {
	line := renderStartupBanner(12*time.Millisecond, false)

	for _, fact := range []string{"12ms", "5m wall", "1m cpu", "1G file", "4096 open"} {
		if !strings.Contains(line, fact) {
			t.Errorf("expected %q in %q", fact, line)
		}
	}
}

func TestAResumedConversationHasNoStartupLine(t *testing.T) {
	line := renderStartupBanner(time.Millisecond, true)

	if line != "" {
		t.Errorf("expected no startup line, got %q", line)
	}
}
