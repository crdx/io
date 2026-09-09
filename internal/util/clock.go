package util

import "time"

func WallClock(when time.Time) time.Time {
	return when.Round(0)
}
