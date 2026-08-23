package main

import (
	"testing"

	"crdx.org/io/agent"
)

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
