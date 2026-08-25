package anthropic

import (
	"context"
	"strings"
	"time"

	"crdx.org/io/agent"
)

const (
	turnSuffix  = "/v1/messages"
	usageSuffix = "/api/oauth/usage"
)

const (
	sessionWindow = 5 * time.Hour
	weeklyWindow  = 7 * 24 * time.Hour
)

type usageLimit struct {
	Group    string  `json:"group"`
	Percent  float64 `json:"percent"`
	ResetsAt string  `json:"resets_at"`
	Scope    *struct {
		Model *struct {
			DisplayName *string `json:"display_name"`
		} `json:"model"`
	} `json:"scope"`
}

func (self *Client) IsAvailable() bool {
	_, isAvailable := usageAddress(self.URL)

	return isAvailable
}

func (self *Client) UsageWindows(ctx context.Context) ([]agent.UsageWindow, error) {
	address, reportable := usageAddress(self.URL)
	if !reportable {
		return nil, nil
	}

	token, err := self.tokens.Token()
	if err != nil {
		return nil, err
	}

	var payload struct {
		Limits []usageLimit `json:"limits"`
	}

	if err := self.observedRequests().Get(ctx, address, self.headers(token), &payload); err != nil {
		return nil, err
	}

	return usageWindows(payload.Limits), nil
}

func usageWindows(limits []usageLimit) []agent.UsageWindow {
	var unscoped, scoped []agent.UsageWindow

	for _, limit := range limits {
		resetsAt, err := time.Parse(time.RFC3339, limit.ResetsAt)
		if err != nil {
			continue
		}

		scopeName := ""
		if limit.Scope != nil && limit.Scope.Model != nil && limit.Scope.Model.DisplayName != nil {
			scopeName = strings.ToLower(*limit.Scope.Model.DisplayName)
		}

		switch {
		case limit.Group == "session":
			unscoped = append(unscoped, agent.UsageWindow{
				Duration: sessionWindow,
				Percent:  limit.Percent,
				ResetsAt: resetsAt,
			})

		case limit.Group == "weekly" && limit.Scope == nil:
			unscoped = append(unscoped, agent.UsageWindow{
				Duration: weeklyWindow,
				Percent:  limit.Percent,
				ResetsAt: resetsAt,
			})

		case limit.Group == "weekly" && scopeName != "":
			scoped = append(scoped, agent.UsageWindow{
				Duration: weeklyWindow,
				Percent:  limit.Percent,
				ResetsAt: resetsAt,
				Scope:    scopeName,
			})
		}
	}

	return append(unscoped, scoped...)
}

func usageAddress(turnAddress string) (string, bool) {
	prefix, found := strings.CutSuffix(turnAddress, turnSuffix)
	if !found {
		return "", false
	}

	return prefix + usageSuffix, true
}
