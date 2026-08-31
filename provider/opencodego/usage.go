package opencodego

import (
	"context"
	"time"

	"crdx.org/io/agent"
)

const (
	EndpointURL      = "https://opencode.ai/zen/go/v1/chat/completions"
	UsageEndpointURL = "https://opencode.ai/zen/go/v1/usage"
)

const (
	rollingWindow = 5 * time.Hour
	weeklyWindow  = 7 * 24 * time.Hour
	monthlyWindow = 30 * 24 * time.Hour
)

const usageOK = "ok"

type usageLimit struct {
	Status   string  `json:"status"`
	Percent  float64 `json:"percent"`
	ResetsAt string  `json:"resetsAt"`
}

func (self *Client) IsAvailable() bool {
	return self.UsageURL != ""
}

func (self *Client) UsageWindows(ctx context.Context) ([]agent.UsageWindow, error) {
	if self.UsageURL == "" {
		return nil, nil
	}

	var payload struct {
		Usage struct {
			Rolling usageLimit `json:"rolling"`
			Weekly  usageLimit `json:"weekly"`
			Monthly usageLimit `json:"monthly"`
		} `json:"usage"`
	}

	header := self.headers()
	header.Set("Accept", "application/json")

	if err := self.observedRequests().Get(ctx, self.UsageURL, header, &payload); err != nil {
		return nil, err
	}

	var windows []agent.UsageWindow

	for _, reportedWindow := range []struct {
		limit    usageLimit
		duration time.Duration
	}{
		{limit: payload.Usage.Rolling, duration: rollingWindow},
		{limit: payload.Usage.Weekly, duration: weeklyWindow},
		{limit: payload.Usage.Monthly, duration: monthlyWindow},
	} {
		if window, ok := usageWindow(reportedWindow.limit, reportedWindow.duration); ok {
			windows = append(windows, window)
		}
	}

	return windows, nil
}

func usageWindow(limit usageLimit, duration time.Duration) (agent.UsageWindow, bool) {
	if limit.Status == "" {
		return agent.UsageWindow{}, false
	}

	window := agent.UsageWindow{
		Duration:  duration,
		Percent:   limit.Percent,
		IsLimited: limit.Status != usageOK,
	}

	if resetsAt, err := time.Parse(time.RFC3339, limit.ResetsAt); err == nil {
		window.ResetsAt = resetsAt
	}

	return window, true
}
