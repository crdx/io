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
