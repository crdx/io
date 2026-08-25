package codex

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"time"

	"crdx.org/io/agent"
)

const (
	primaryUsedHeader     = "X-Codex-Primary-Used-Percent"
	primaryWindowHeader   = "X-Codex-Primary-Window-Minutes"
	primaryResetHeader    = "X-Codex-Primary-Resets-In-Seconds"
	secondaryUsedHeader   = "X-Codex-Secondary-Used-Percent"
	secondaryWindowHeader = "X-Codex-Secondary-Window-Minutes"
	secondaryResetHeader  = "X-Codex-Secondary-Resets-In-Seconds"
)

func (self *Client) IsAvailable() bool {
	self.usageMutex.Lock()
	defer self.usageMutex.Unlock()

	return self.usageWindows != nil
}

func (self *Client) UsageWindows(context.Context) ([]agent.UsageWindow, error) {
	self.usageMutex.Lock()
	defer self.usageMutex.Unlock()

	return slices.Clone(self.usageWindows), nil
}

func (self *Client) recordUsageWindows(header http.Header, now time.Time) {
	var windows []agent.UsageWindow

	for _, names := range [][3]string{
		{primaryUsedHeader, primaryWindowHeader, primaryResetHeader},
		{secondaryUsedHeader, secondaryWindowHeader, secondaryResetHeader},
	} {
		if window, ok := usageWindow(header, names[0], names[1], names[2], now); ok {
			windows = append(windows, window)
		}
	}

	if windows == nil {
		return
	}

	self.usageMutex.Lock()
	defer self.usageMutex.Unlock()

	self.usageWindows = windows
}

func usageWindow(
	header http.Header, usedName string, windowName string, resetName string, now time.Time,
) (agent.UsageWindow, bool) {
	used, err := strconv.ParseFloat(header.Get(usedName), 64)
	if err != nil {
		return agent.UsageWindow{}, false
	}

	minutes, err := strconv.Atoi(header.Get(windowName))
	if err != nil || minutes <= 0 {
		return agent.UsageWindow{}, false
	}

	window := agent.UsageWindow{
		Duration: time.Duration(minutes) * time.Minute,
		Percent:  used,
	}

	if seconds, err := strconv.Atoi(header.Get(resetName)); err == nil && seconds > 0 {
		window.ResetsAt = now.Add(time.Duration(seconds) * time.Second)
	}

	return window, true
}
