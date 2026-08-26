package call

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"crdx.org/io/cmd/oh/dynamic"
	"crdx.org/io/cmd/oh/markdown"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/oh/width"
	"crdx.org/io/internal/util"
	"crdx.org/io/tool"
)

const (
	durationWidth    = 6
	bytesPerMegabyte = 1 << 20
)

type Label struct {
	Name            string
	Subject         string
	Emphasis        tool.Emphasis
	Qualifier       string
	ReadOnly        bool
	NameStyle       style.Style
	Accent          string
	AccentStyle     style.Style
	renderedSubject string
}

func (self Label) Elide(room int) dynamic.Label {
	self.renderedSubject = ""
	self.Name = width.Elide(self.Name, room)
	room -= width.Of(self.Name) + 1

	if room > 0 {
		completeSubject := self.Subject
		emphasisSource := self.getSource()
		self.Subject = width.Elide(self.Subject, room)
		if self.Emphasis.Kind == tool.EmphasisSyntax && self.Subject != completeSubject {
			subject := strings.TrimSuffix(self.Subject, width.Ellipsis)
			self.renderedSubject = markdown.Highlight(emphasisSource, subject, self.Emphasis.Value, true)
		}
		room -= width.Of(self.Subject) + 1
	} else {
		self.Subject = ""
		room = 0
	}

	if room > 0 {
		self.Qualifier = width.Elide(self.Qualifier, room)
	} else {
		self.Qualifier = ""
	}

	return self
}

func (self Label) Render() string {
	line := self.style()(self.Name)

	if self.Subject != "" {
		line += " " + self.renderSubject()
	}

	if self.Qualifier != "" {
		line += " " + self.renderQualifier()
	}

	return line
}

func (self Label) Width() int {
	total := width.Of(self.Name)

	if self.Subject != "" {
		total += 1 + width.Of(self.Subject)
	}

	if self.Qualifier != "" {
		total += 1 + width.Of(self.Qualifier)
	}

	return total
}

func (self Label) getSource() string {
	if self.Emphasis.Source != "" {
		return self.Emphasis.Source
	}

	return self.Subject
}

func (self Label) renderSubject() string {
	if self.renderedSubject != "" {
		return self.renderedSubject
	}
	if self.Emphasis.Kind == tool.EmphasisSyntax {
		return markdown.Highlight(self.getSource(), self.Subject, self.Emphasis.Value, false)
	}

	type span struct {
		start int
		end   int
		style style.Style
	}

	spans := []span{}
	focus := self.focus()
	if at := strings.LastIndex(self.Subject, focus); focus != "" && at >= 0 {
		spans = append(spans, span{start: at, end: at + len(focus), style: style.Subject})
	}
	if at := strings.LastIndex(self.Subject, self.Accent); self.Accent != "" && self.AccentStyle != nil && at >= 0 {
		spans = append(spans, span{start: at, end: at + len(self.Accent), style: self.AccentStyle})
	}

	if len(spans) == 0 {
		return style.Subject(self.Subject)
	}

	sort.Slice(spans, func(i int, j int) bool {
		return spans[i].start < spans[j].start
	})

	var out strings.Builder
	at := 0
	for _, marked := range spans {
		if marked.start < at {
			continue
		}
		out.WriteString(style.Subtle(self.Subject[at:marked.start]))
		out.WriteString(marked.style(self.Subject[marked.start:marked.end]))
		at = marked.end
	}
	if at < len(self.Subject) {
		out.WriteString(style.Subtle(self.Subject[at:]))
	}

	return out.String()
}

func (self Label) focus() string {
	if self.Emphasis.Kind == tool.EmphasisFocus {
		return self.Emphasis.Value
	}

	return ""
}

func (self Label) renderQualifier() string {
	focus := self.focus()
	if focus == "" || strings.Contains(self.Subject, focus) {
		return style.Qualifier(self.Qualifier)
	}

	at := strings.LastIndex(self.Qualifier, focus)
	if at < 0 {
		return style.Qualifier(self.Qualifier)
	}

	end := at + len(focus)

	var out strings.Builder
	if at > 0 {
		out.WriteString(style.Qualifier(self.Qualifier[:at]))
	}
	out.WriteString(style.Subject(self.Qualifier[at:end]))
	if end < len(self.Qualifier) {
		out.WriteString(style.Qualifier(self.Qualifier[end:]))
	}

	return out.String()
}

func (self Label) style() style.Style {
	if self.NameStyle != nil {
		return self.NameStyle
	}
	if self.ReadOnly {
		return style.Call
	}

	return style.Change
}

func Measurements(took time.Duration, stats *tool.Stats) string {
	if stats == nil {
		return ""
	}

	switch stats.Kind {
	case tool.StatsOutput:
		return outputStatsText(stats)
	case tool.StatsResources:
		return resourcesStatsText(took, stats)
	case tool.StatsRead:
		return readStatsText(stats)
	case tool.StatsList:
		return listStatsText(stats)
	case tool.StatsImage:
		return imageStatsText(stats)
	case tool.StatsWrite:
		return writeStatsText(stats)
	case tool.StatsDiff:
		return diffStatsText(stats)
	case tool.StatsSearch:
		return searchStatsText(stats)
	}

	return ""
}

func outputStatsText(stats *tool.Stats) string {
	if stats.Bytes == 0 && stats.Lines == 0 {
		return style.Subtle("no output")
	}

	truncationMarker := ""
	if stats.Truncated {
		truncationMarker = "+"
	}
	return subtleStats(fmt.Sprintf("%dL%s", stats.Lines, truncationMarker), tokenEstimate(stats))
}

func resourcesStatsText(took time.Duration, stats *tool.Stats) string {
	return subtleStats(
		fmt.Sprint(stats.Lines)+"L",
		tokenEstimate(stats),
		util.CompactDuration(took),
		util.CompactDuration(stats.CPUTime),
		fmt.Sprint(stats.PeakMemory/bytesPerMegabyte)+"M",
	)
}

func readStatsText(stats *tool.Stats) string {
	return subtleStats(fmt.Sprint(stats.Lines)+"L", tokenEstimate(stats))
}

func listStatsText(stats *tool.Stats) string {
	return style.Subtle(fmt.Sprint(stats.Lines) + "L")
}

func imageStatsText(stats *tool.Stats) string {
	return style.Subtle(util.FormatEstimatedTokenCount(stats.EstimatedTokens))
}

func writeStatsText(stats *tool.Stats) string {
	return subtleStats(fmt.Sprint(stats.Lines)+"L", tokenEstimate(stats))
}

func diffStatsText(stats *tool.Stats) string {
	return style.Success("+%d", stats.Added) +
		style.Subtle(" ") + style.Failure("−%d", stats.Removed)
}

func searchStatsText(stats *tool.Stats) string {
	capMarker := ""
	if stats.Truncated {
		capMarker = "+"
	}
	return subtleStats(fmt.Sprintf("%dL%s", stats.Lines, capMarker), tokenEstimate(stats))
}

func subtleStats(parts ...string) string {
	kept := parts[:0]
	for _, part := range parts {
		if part != "" {
			kept = append(kept, part)
		}
	}
	return style.Subtle(strings.Join(kept, " "))
}

func tokenEstimate(stats *tool.Stats) string {
	const maximumHiddenTokenEstimate = 100

	returnedTokens := util.EstimateTokenCount(stats.Bytes)
	if returnedTokens > maximumHiddenTokenEstimate {
		returned := util.FormatEstimatedTokenCount(returnedTokens)
		if stats.TotalBytes > stats.Bytes {
			return returned + " (of " + util.FormatTokenEstimate(stats.TotalBytes) + ")"
		}
		return returned
	}

	if stats.TotalBytes > stats.Bytes && util.EstimateTokenCount(stats.TotalBytes) > maximumHiddenTokenEstimate {
		return "(of " + util.FormatTokenEstimate(stats.TotalBytes) + ")"
	}
	return ""
}
