// Package status draws the rows a set of tool calls is shown on while they run.
package status

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"crdx.org/io/cmd/oh/markdown"
	"crdx.org/io/cmd/oh/spinner"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/oh/width"
	"crdx.org/io/internal/util"
	"crdx.org/io/tool"
)

const (
	reveal           = time.Second
	durationWidth    = 6
	patience         = 5 * time.Second
	bytesPerMegabyte = 1 << 20
)

// State is a call.
type State int

// What a row can say about a call: it is running, it finished, it failed, or it never got anywhere.
const (
	Running State = iota
	Done
	Failed
	Cancelled
)

// Label is what a row says: the name of a call, its subject, and whatever qualifies that.
type Label struct {
	Name            string
	Subject         string
	Highlight       tool.Highlight
	Qualifier       string
	ReadOnly        bool        // whether the call changes nothing, which decides the colour its name is in
	NameStyle       style.Style // an explicit style for a tool with its own prompt
	Accent          string      // another part of the subject set apart from the rest
	AccentStyle     style.Style // how the accent is painted
	renderedSubject string      // syntax-highlighted subject retained from the complete source
}

// Elide cuts a label to the room it has, so the row stays on the line it was printed on. What
// qualifies the subject is the first to go, being the least of it.
func (self Label) Elide(room int) Label {
	self.renderedSubject = ""
	self.Name = elide(self.Name, room)
	room -= width.Of(self.Name) + 1

	if room > 0 {
		completeSubject := self.Subject
		self.Subject = elide(self.Subject, room)
		if self.Highlight.Kind == tool.HighlightSyntax && self.Subject != completeSubject {
			prefix := strings.TrimSuffix(self.Subject, ellipsis)
			self.renderedSubject = markdown.HighlightPrefix(completeSubject, prefix, self.Highlight.Value, true)
		}
		room -= width.Of(self.Subject) + 1
	} else {
		self.Subject = ""
		room = 0
	}

	if room > 0 {
		self.Qualifier = elide(self.Qualifier, room)
	} else {
		self.Qualifier = ""
	}

	return self
}

func (self Label) render() string {
	line := self.style()(self.Name)

	if self.Subject != "" {
		line += " " + self.renderSubject()
	}

	if self.Qualifier != "" {
		line += " " + self.renderQualifier()
	}

	return line
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

func (self Label) width() int {
	total := width.Of(self.Name)

	if self.Subject != "" {
		total += 1 + width.Of(self.Subject)
	}

	if self.Qualifier != "" {
		total += 1 + width.Of(self.Qualifier)
	}

	return total
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

const ellipsis = "…"

func elide(text string, room int) string {
	if room <= 0 {
		return ""
	}

	if width.Of(text) <= room {
		return text
	}

	if room == 1 {
		return ellipsis
	}

	nonEmptyParts, _ := width.Cut(text, room-1)

	return nonEmptyParts + ellipsis
}

type row struct {
	label     Label
	state     State // how the call ended
	startedAt time.Time
	took      time.Duration
	failure   string // why it ended that way, where that was a failure
	stats     *tool.Stats
}

// Block displays and redraws a group of tool-call rows. Nothing else may print until it closes.
type Block struct {
	print   func(string)
	overlay func(string, int) // redraws existing rows
	isLive  bool              // whether rows may be redrawn
	columns int

	mutex      sync.Mutex // guards the changing rows
	rows       []row
	frame      int  // the spinner frame
	isRevealed bool // whether elapsed times are shown

	stop     chan struct{}  // asks the ticker to stop
	stopWait sync.WaitGroup // waits for the ticker to end
}

// New opens an empty block. Non-live blocks ignore redraws.
func New(print func(string), overlay func(string, int), isLive bool, columns int) *Block {
	self := &Block{
		print:   print,
		overlay: overlay,
		isLive:  isLive,
		columns: columns,
		stop:    make(chan struct{}),
	}

	self.stopWait.Add(1)

	go self.run()

	return self
}

// Add puts a call on the block and hands back the row it went on.
func (self *Block) Add(label Label) int {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.rows = append(self.rows, row{label: label, startedAt: time.Now()})

	index := len(self.rows) - 1

	if index > 0 {
		self.print("\n")
	}

	self.print(self.line(self.rows[index]))

	return index
}

// MarkWithStats marks a call and includes measurements made by its tool.
func (self *Block) MarkWithStats(
	index int,
	state State,
	took time.Duration,
	reason string,
	stats *tool.Stats,
) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if index < 0 || index >= len(self.rows) || self.rows[index].state != Running {
		return
	}

	self.rows[index].state = state
	self.rows[index].took = took
	self.rows[index].stats = stats

	if state == Failed {
		self.rows[index].failure = collapse(reason)
	}

	self.redraw()
}

func collapse(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// Stop ends the block where it stands, saying nothing more about it. What was drawn is left on the
// screen for whatever is about to take its place.
func (self *Block) Stop() {
	close(self.stop)
	self.stopWait.Wait()
}

// Close ends the block, marking anything still running with what became of it.
func (self *Block) Close(state State) {
	self.Stop()

	self.mutex.Lock()
	defer self.mutex.Unlock()

	for i := range self.rows {
		if self.rows[i].state == Running {
			self.rows[i].state = state
			self.rows[i].took = time.Since(self.rows[i].startedAt)
		}
	}

	self.isRevealed = false

	self.redraw()
}

func (self *Block) run() {
	defer self.stopWait.Done()

	select {
	case <-self.stop:
		return
	case <-time.After(reveal):
	}

	self.mutex.Lock()
	self.isRevealed = true
	self.redraw()
	self.mutex.Unlock()

	ticker := time.NewTicker(spinner.Activity.Rate())
	defer ticker.Stop()

	for {
		select {
		case <-self.stop:
			return
		case <-ticker.C:
			self.mutex.Lock()
			self.frame++
			self.redraw()
			self.mutex.Unlock()
		}
	}
}

func (self *Block) redraw() {
	if !self.isLive || len(self.rows) == 0 {
		return
	}

	lines := make([]string, len(self.rows))

	for i, item := range self.rows {
		lines[i] = "\r\x1b[K" + self.line(item)
	}

	up := ""
	if len(self.rows) > 1 {
		up = fmt.Sprintf("\x1b[%dA", len(self.rows)-1)
	}

	self.overlay(up+strings.Join(lines, "\n"), style.Width(lines[len(lines)-1]))
}

const (
	failureShare = 2
	edgeGuard    = 2
)

func (self *Block) line(item row) string {
	outcome := self.outcome(item)

	label := item.label
	failure := item.failure

	if self.columns > 0 {
		room := self.columns - edgeGuard - style.Width(outcome) - outcomeSpacing(outcome)

		if failure != "" {
			spare := max(room-label.width()-1, room/failureShare)
			failure = elide(failure, min(width.Of(failure), spare))
			room -= width.Of(failure) + 1
		}

		label = label.Elide(room)
	}

	return util.JoinNonEmpty(label.render(), outcome, failureText(failure))
}

func failureText(failure string) string {
	if failure == "" {
		return ""
	}

	return style.Failure(failure)
}

func outcomeSpacing(outcome string) int {
	if outcome == "" {
		return 0
	}

	return 1
}

func (self *Block) outcome(item row) string {
	if item.state == Running {
		if !self.isRevealed {
			return ""
		}

		elapsed := time.Since(item.startedAt).Truncate(time.Second)
		return outcomeText(style.Spinner(spinner.Activity.Frame(self.frame)), elapsed, nil)
	}

	return outcomeText(glyph(item.state), item.took, item.stats)
}

func outcomeText(mark string, took time.Duration, stats *tool.Stats) string {
	if stats == nil {
		if took < patience {
			return mark
		}
		return mark + " " + style.Spinner(util.CompactDuration(took))
	}

	var statsText string
	switch stats.Kind {
	case tool.StatsOutput:
		statsText = outputStatsText(stats)
	case tool.StatsResources:
		statsText = resourcesStatsText(took, stats)
	case tool.StatsRead:
		statsText = readStatsText(stats)
	case tool.StatsList:
		statsText = listStatsText(stats)
	case tool.StatsImage:
		statsText = imageStatsText(stats)
	case tool.StatsWrite:
		statsText = writeStatsText(stats)
	case tool.StatsDiff:
		statsText = diffStatsText(stats)
	case tool.StatsSearch:
		statsText = searchStatsText(stats)
	}

	if statsText == "" {
		return mark
	}
	return mark + " " + statsText
}

func outputStatsText(stats *tool.Stats) string {
	if stats.Bytes == 0 && stats.Lines == 0 {
		return style.Subtle("no output")
	}

	capMarker := ""
	if stats.Truncated {
		capMarker = "+"
	}
	return style.Subtle(fmt.Sprintf("%dL%s %s", stats.Lines, capMarker, tokenEstimate(stats)))
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

func glyph(state State) string {
	switch state {
	case Failed:
		return style.Failure("✗")
	case Cancelled:
		return style.Cancelled("–")
	case Done, Running:
		return style.Success("✓")
	}

	return ""
}
