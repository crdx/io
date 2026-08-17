package main

import (
	"fmt"
	"strings"
	"time"
)

var startedAt = time.Now()

func renderStartupBanner(elapsed time.Duration, resumed bool) string {
	if resumed {
		return ""
	}

	return strings.Join([]string{timeTaken(elapsed), limits()}, ", ")
}

func timeTaken(elapsed time.Duration) string {
	if elapsed < time.Millisecond {
		return elapsed.Round(time.Microsecond).String()
	}

	return elapsed.Round(time.Millisecond).String()
}

func limits() string { // the same whatever is granted, so no policy is asked for one
	return fmt.Sprintf(
		"%s wall, %s cpu, %s file, %d open",
		formatDuration(shellTimeout),
		formatDuration(shellCPUTime),
		size(shellFileSize),
		shellOpenFiles,
	)
}

func formatDuration(limit time.Duration) string {
	switch {
	case limit <= 0:
		return "none"
	case limit%time.Minute == 0:
		return fmt.Sprintf("%dm", int(limit.Minutes()))
	default:
		return fmt.Sprintf("%ds", int(limit.Seconds()))
	}
}

const (
	megabyte = 1 << 20
	gigabyte = 1 << 30
)

func size(limit int64) string {
	switch {
	case limit <= 0:
		return "none"
	case limit >= gigabyte:
		return fmt.Sprintf("%.4gG", float64(limit)/gigabyte)
	default:
		return fmt.Sprintf("%dM", limit/megabyte)
	}
}
