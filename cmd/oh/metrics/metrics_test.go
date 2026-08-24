package metrics

import (
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"crdx.org/io/agent"
)

func contextUsageAt(events []agent.Event, contextWindowTokens int) (int, int) {
	tracker := New(contextWindowTokens)
	tracker.Restore(events)
	return tracker.ContextUsage()
}

func TestContextUsageComesFromTheLatestReportInTheViewedHistory(t *testing.T) {
	events := []agent.Event{
		{Kind: agent.ModelMessageEvent, Usage: &agent.Usage{InputTokens: 5000}},
		{Kind: agent.UserMessageEvent},
		{Kind: agent.ToolCallRequestEvent, Usage: &agent.Usage{InputTokens: 12_000}},
	}

	usedTokens, totalTokens := contextUsageAt(events[:2], 200_000)
	if usedTokens != 5000 || totalTokens != 200_000 {
		t.Errorf("historical prefix gave %d/%d", usedTokens, totalTokens)
	}

	usedTokens, totalTokens = contextUsageAt(events, 200_000)
	if usedTokens != 12_000 || totalTokens != 200_000 {
		t.Errorf("full history gave %d/%d", usedTokens, totalTokens)
	}
}

func TestContextUsageIsUnknownBeforeTheFirstReport(t *testing.T) {
	usedTokens, totalTokens := contextUsageAt([]agent.Event{{Kind: agent.UserMessageEvent}}, 200_000)
	if usedTokens != 0 || totalTokens != 200_000 {
		t.Errorf("got %d/%d", usedTokens, totalTokens)
	}
}

func TestTrackerCountsEveryUserTurn(t *testing.T) {
	tracker := New(0)
	for range 3 {
		tracker.Record(agent.Event{Kind: agent.UserMessageEvent, Text: "go on then"})
		tracker.Record(agent.Event{Kind: agent.ModelMessageEvent, Text: "right you are"})
	}

	if got := tracker.TurnCount(); got != 3 {
		t.Errorf("expected three turns, got %d", got)
	}
}

func TestTrackerMeasuresTheLatestTurnTokenRate(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tracker := New(0)
		tracker.BeginTurn()
		startedAt := time.Now()
		tracker.Record(agent.Event{Kind: agent.ModelReasoningEvent, Text: strings.Repeat("x", 200)})
		tracker.Record(agent.Event{Kind: agent.ModelMessageEvent, Text: strings.Repeat("x", 200)})

		time.Sleep(10 * time.Second)
		tracker.FinishTurn(startedAt)

		tokensPerSecond, known := tracker.LastTurnTokenRate()
		if !known || tokensPerSecond != 10 {
			t.Errorf("expected ten tokens a second, got %v (known: %v)", tokensPerSecond, known)
		}
	})
}

func TestSilentTurnKeepsThePreviousTokenRate(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tracker := New(0)
		tracker.BeginTurn()
		firstStartedAt := time.Now()
		tracker.Record(agent.Event{Kind: agent.ModelMessageEvent, Text: strings.Repeat("x", 400)})
		time.Sleep(10 * time.Second)
		tracker.FinishTurn(firstStartedAt)

		tracker.BeginTurn()
		time.Sleep(time.Second)
		tracker.FinishTurn(time.Now().Add(-time.Second))

		if tokensPerSecond, _ := tracker.LastTurnTokenRate(); tokensPerSecond != 10 {
			t.Errorf("expected a silent turn to leave the rate alone, got %v", tokensPerSecond)
		}
	})
}
