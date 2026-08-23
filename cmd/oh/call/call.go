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
	Highlight       tool.Highlight
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
		self.Subject = width.Elide(self.Subject, room)
		if self.Highlight.Kind == tool.HighlightSyntax && self.Subject != completeSubject {
			prefix := strings.TrimSuffix(self.Subject, width.Ellipsis)
			self.renderedSubject = markdown.HighlightPrefix(completeSubject, prefix, self.Highlight.Value, true)
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

func (self Label) renderSubject() string {
	if self.renderedSubject != "" {
		return self.renderedSubject
	}
	if self.Highlight.Kind == tool.HighlightSyntax {
		return markdown.Highlight(self.Subject, self.Highlight.Value)
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

	sort.Slice(spans, func(i int, j int) bool { return spans[i].start < spans[j].start })

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
	if self.Highlight.Kind == tool.HighlightFocus {
		return self.Highlight.Value
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
	return style.Subtle(fmt.Sprintf("%dL%s %s", stats.Lines, truncationMarker, tokenEstimate(stats)))
}

func resourcesStatsText(took time.Duration, stats *tool.Stats) string {
	return style.Subtle(fmt.Sprintf(
		"%dL %s %s %s %dM",
		stats.Lines,
		tokenEstimate(stats),
		util.CompactDuration(took),
		util.CompactDuration(stats.CPUTime),
		stats.PeakMemory/bytesPerMegabyte,
	))
}

func readStatsText(stats *tool.Stats) string {
	return style.Subtle(fmt.Sprint(stats.Lines) + "L " + tokenEstimate(stats))
}

func listStatsText(stats *tool.Stats) string {
	return style.Subtle(fmt.Sprint(stats.Lines) + "L")
}

func imageStatsText(stats *tool.Stats) string {
	return style.Subtle(util.FormatEstimatedTokenCount(stats.EstimatedTokens, 2))
}

func writeStatsText(stats *tool.Stats) string {
	return style.Subtle(fmt.Sprint(stats.Lines) + "L " + tokenEstimate(stats))
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
	return style.Subtle(fmt.Sprintf("%dL%s %s", stats.Lines, capMarker, tokenEstimate(stats)))
}

func tokenEstimate(stats *tool.Stats) string {
	returned := util.FormatTokenEstimate(stats.Bytes, 2)
	if stats.TotalBytes > stats.Bytes {
		return returned + " (of " + util.FormatTokenEstimate(stats.TotalBytes, 2) + ")"
	}
	return returned
}
