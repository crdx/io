package usage

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"crdx.org/io/cmd/oh/style"
)

const (
	gaugeWidth = 16
	paceWidth  = 5

	fillMark    = "█"
	trackMark   = "░"
	tickMark    = "┃"
	freshMark   = "●"
	waitingMark = "◷"
	silentMark  = "○"
	failureMark = "✖"
	aheadMark   = "▲"
	behindMark  = "▼"
	evenMark    = "▪"

	idleLabel    = "idle"
	staleLabel   = "stale"
	limitedLabel = "limited"
)

func Render(report Report, now time.Time, drawing *Graphics) string {
	sections := make([]string, 0, len(report.Providers))

	labelWidth := 0
	for _, provider := range report.Providers {
		for _, limit := range provider.Limits {
			labelWidth = max(labelWidth, style.Width(limit.Label))
		}
	}

	for _, provider := range report.Providers {
		if provider.Status == StatusOK && len(provider.Limits) == 0 {
			continue
		}

		sections = append(sections, renderProvider(provider, now, labelWidth, drawing))
	}

	if len(sections) == 0 {
		return style.Dim("no usage to report") + "\n"
	}

	return strings.Join(sections, "\n\n") + "\n"
}

func renderProvider(provider Snapshot, now time.Time, labelWidth int, drawing *Graphics) string {
	lines := []string{renderHeader(provider, now)}

	for _, limit := range provider.Limits {
		pace := paceOf(limit)

		parts := []string{
			style.Normal(pad(limit.Label, labelWidth)),
			paceStyle(pace)(fmt.Sprintf("%3d%%", limit.UsedPercent)),
			renderGauge(limit, pace, gaugeWidth, drawing),
		}

		reset := renderReset(limit, now)

		switch {
		case limit.StateAt(now) == StateStale:
			parts = append(parts, pad("", paceWidth), reset)
		case reset != "":
			parts = append(parts, paceStyle(pace)(pad(paceText(limit), paceWidth)), reset)
		default:
			if text := paceText(limit); text != "" {
				parts = append(parts, paceStyle(pace)(text))
			}
		}

		lines = append(lines, strings.Join(parts, " "))
	}

	return strings.Join(lines, "\n")
}

func renderHeader(provider Snapshot, now time.Time) string {
	name := style.Information(provider.Provider.Label)

	if provider.Status == StatusFailed {
		return style.Failure(failureMark) + " " + name + " " + style.Dim(provider.Message)
	}

	if provider.Status != StatusOK {
		return style.Dim(silentMark) + " " + name + " " + style.Dim(provider.Message)
	}

	age, isMeasured := ageOf(provider, now)
	if !isMeasured {
		return style.Dim(freshMark) + " " + name
	}

	mark, appearance, isAgeWorthSaying := freshness(age, provider)
	if !isAgeWorthSaying {
		return appearance(mark) + " " + name
	}

	return appearance(mark) + " " + name + " " + countdown(age)
}

func ageOf(provider Snapshot, now time.Time) (time.Duration, bool) {
	measuredAt, isMeasured := provider.MeasuredTime()
	if !isMeasured {
		return 0, false
	}

	return max(0, now.Sub(measuredAt)), true
}

func freshness(age time.Duration, provider Snapshot) (string, style.Style, bool) {
	switch provider.FreshnessAt(age) {
	case FreshnessDue:
		return freshMark, style.Change, true
	case FreshnessStale:
		return freshMark, style.Failure, true
	case FreshnessWaiting:
		return waitingMark, style.Change, true
	default:
		return freshMark, style.Success, false
	}
}

func renderGauge(limit Limit, pace Pace, cells int, drawing *Graphics) string {
	if drawing != nil {
		if placement, isPlaced := gaugePlacement(limit, pace, cells, *drawing); isPlaced {
			return placement
		}
	}

	fillCells := limit.UsedPercent * cells / percentCeiling

	tickColumn := -1
	if limit.ExpectedPercent != nil {
		tickColumn = min(cells-1, *limit.ExpectedPercent*cells/percentCeiling)
	}

	var gauge strings.Builder
	var run strings.Builder

	runStyle := style.Dim

	for column := range cells {
		mark, cellStyle := trackMark, style.Dim

		switch {
		case column == tickColumn:
			mark = tickMark
		case column < fillCells:
			mark, cellStyle = fillMark, paceStyle(pace)
		}

		if run.Len() > 0 && !sameStyle(cellStyle, runStyle) {
			gauge.WriteString(runStyle(run.String()))
			run.Reset()
		}

		runStyle = cellStyle
		run.WriteString(mark)
	}

	gauge.WriteString(runStyle(run.String()))

	return gauge.String()
}

func renderReset(limit Limit, now time.Time) string {
	switch limit.StateAt(now) {
	case StateLimited:
		return style.Failure(limitedLabel)
	case StateIdle:
		return style.Dim(idleLabel)
	case StateStale:
		return style.Dim(staleLabel)
	case StateUnknown:
		return ""
	}

	resetsAt, hasReset := limit.ResetTime()
	if !hasReset {
		return ""
	}

	return countdown(resetsAt.Sub(now))
}

func countdown(remainingTime time.Duration) string {
	switch {
	case remainingTime >= dayLength:
		return spans(remainingTime/dayLength, "d", int(remainingTime.Hours())%24, "h")
	case remainingTime >= time.Hour:
		return spans(time.Duration(remainingTime.Hours()), "h", int(remainingTime.Minutes())%60, "m")
	case remainingTime >= time.Minute:
		return spans(time.Duration(remainingTime.Minutes()), "m", int(remainingTime.Seconds())%60, "s")
	default:
		return style.Normal(fmt.Sprintf("%ds", int(remainingTime.Seconds())))
	}
}

func spans(major time.Duration, majorUnit string, minor int, minorUnit string) string {
	return style.Normal(fmt.Sprintf("%d%s", major, majorUnit)) +
		style.Dim(fmt.Sprintf("%02d%s", minor, minorUnit))
}

func paceText(limit Limit) string {
	if limit.ExpectedPercent == nil {
		return ""
	}

	delta := limit.UsedPercent - *limit.ExpectedPercent

	switch {
	case delta > 0:
		return aheadMark + " " + strconv.Itoa(delta)
	case delta < 0:
		return behindMark + " " + strconv.Itoa(-delta)
	default:
		return evenMark
	}
}

func paceOf(limit Limit) Pace {
	switch limit.Severity {
	case SeverityWarning:
		return PaceAhead
	case SeverityError:
		return PaceCritical
	default:
		return PaceEven
	}
}

func paceStyle(pace Pace) style.Style {
	switch pace {
	case PaceAhead:
		return style.Change
	case PaceCritical:
		return style.Failure
	case PaceEven:
	}

	return style.Information
}

func sameStyle(left style.Style, right style.Style) bool {
	const marker = "\x00"

	return left(marker) == right(marker)
}

func pad(text string, cells int) string {
	if gap := cells - style.Width(text); gap > 0 {
		return text + strings.Repeat(" ", gap)
	}

	return text
}
