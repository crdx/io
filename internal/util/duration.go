package util

import (
	"fmt"
	"time"
)

func CompactDuration(took time.Duration) string {
	if took > -time.Second && took < time.Second {
		tenths := took.Round(100 * time.Millisecond).Seconds()
		if tenths == 0 {
			return "0s"
		}
		return fmt.Sprintf("%.1fs", tenths)
	}
	switch {
	case took%time.Second == 0 && took < time.Minute:
		return fmt.Sprintf("%ds", int(took.Seconds()))
	case took%time.Minute == 0 && took < time.Hour:
		return fmt.Sprintf("%dm", int(took.Minutes()))
	case took%time.Hour == 0 && took < 100*time.Hour:
		return fmt.Sprintf("%dh", int(took.Hours()))
	}
	return FormatDuration(took)
}

func CoarseDuration(elapsedTime time.Duration) string {
	switch {
	case elapsedTime < time.Minute:
		return "<1m"
	case elapsedTime < time.Hour:
		return fmt.Sprintf("%dm", int(elapsedTime.Minutes()))
	case elapsedTime < 24*time.Hour:
		return fmt.Sprintf("%dh", int(elapsedTime.Hours()))
	}

	return fmt.Sprintf("%dd", int(elapsedTime.Hours()/24))
}

func Ago(when time.Time) string {
	elapsedTime := time.Since(when)
	if elapsedTime < time.Minute {
		return "just now"
	}

	return CoarseDuration(elapsedTime) + " ago"
}

func FormatDuration(took time.Duration) string {
	switch {
	case took < time.Second:
		return fmt.Sprintf("0.%ds", int(took.Milliseconds()%1000)/100)
	case took < time.Minute:
		return fmt.Sprintf("%ds", int(took.Seconds()))
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
