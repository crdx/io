package util

import (
	"fmt"
	"time"
)

// CompactDuration formats short durations precisely and longer durations compactly.
func CompactDuration(took time.Duration) string {
	if took > -time.Second && took < time.Second {
		tenths := took.Round(100 * time.Millisecond).Seconds()
		if tenths == 0 {
			return "0s"
		}
		return fmt.Sprintf("%.1fs", tenths)
	}
	if took%time.Second == 0 && took < time.Minute {
		return fmt.Sprintf("%ds", int(took.Seconds()))
	}
	return FormatDuration(took)
}

// CoarseDuration formats a duration to a single unit.
func CoarseDuration(elapsed time.Duration) string {
	switch {
	case elapsed < time.Minute:
		return "<1m"
	case elapsed < time.Hour:
		return fmt.Sprintf("%dm", int(elapsed.Minutes()))
	case elapsed < 24*time.Hour:
		return fmt.Sprintf("%dh", int(elapsed.Hours()))
	}

	return fmt.Sprintf("%dd", int(elapsed.Hours()/24))
}

// Ago says how long ago a moment was, coarsely, in the past tense.
func Ago(when time.Time) string {
	elapsed := time.Since(when)
	if elapsed < time.Minute {
		return "just now"
	}

	return CoarseDuration(elapsed) + " ago"
}

// FormatDuration formats a duration in no more than six characters.
func FormatDuration(took time.Duration) string {
	switch {
	case took < time.Minute:
		return fmt.Sprintf("%d.%ds", int(took.Seconds()), int(took.Milliseconds()%1000)/100)
	case took < time.Hour:
		return fmt.Sprintf("%dm%02ds", int(took.Minutes()), int(took.Seconds())%60)
	case took < 100*time.Hour:
		return fmt.Sprintf("%dh%02dm", int(took.Hours()), int(took.Minutes())%60)
	}

	days := int(took.Hours()) / 24
	switch {
	case days < 100:
		return fmt.Sprintf("%dd%02dh", days, int(took.Hours())%24)
	case days <= 9999:
		return fmt.Sprintf("%dd", days)
	default:
		return "9999d+"
	}
}
